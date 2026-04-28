package follower

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/pkg/helpers"
)

// streamHTTPClient is reused for stream-push deliveries. No overall timeout
// because file transfers can legitimately take longer than any default.
var streamHTTPClient = &http.Client{
	Timeout: 0,
	Transport: &http.Transport{
		MaxIdleConns:        10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	},
}

// deliverStream resolves the master's stream_request to a local file, applies
// the optional Range header, and POSTs the bytes to the master's stream-push
// endpoint. Token format and path containment are validated before any I/O so
// a hostile master can't drive arbitrary file reads.
func (m *Module) deliverStream(ctx context.Context, cfg config.FollowerConfig, req streamRequest) {
	if !isValidToken(req.Token) {
		m.log.Warn("Stream request rejected: invalid token format %q", req.Token)
		return
	}

	roots := collectMediaRoots(m.config.Get().Directories)
	absPath, err := resolveAndValidate(req.Path, roots)
	if err != nil {
		m.log.Warn("Stream request denied (path=%q): %v", req.Path, err)
		return
	}

	file, err := os.Open(absPath)
	if err != nil {
		m.log.Warn("Failed to open file for stream %s: %v", req.Token, err)
		return
	}
	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil {
		m.log.Warn("Failed to stat file for stream %s: %v", req.Token, err)
		return
	}

	contentType := helpers.MediaContentType(absPath)

	var body io.Reader = file
	statusCode := http.StatusOK
	contentLength := stat.Size()
	var extraHeaders map[string]string

	if req.Range != "" {
		start, end, parseErr := parseRange(req.Range, stat.Size())
		switch {
		case parseErr != nil:
			m.log.Warn("Invalid range header %q for %s: %v; delivering full file", req.Range, req.Token, parseErr)
		default:
			if _, seekErr := file.Seek(start, io.SeekStart); seekErr != nil {
				m.log.Warn("Seek failed for stream %s: %v; delivering full file", req.Token, seekErr)
				_, _ = file.Seek(0, io.SeekStart)
			} else {
				length := end - start + 1
				body = io.LimitReader(file, length)
				statusCode = http.StatusPartialContent
				contentLength = length
				extraHeaders = map[string]string{
					"Content-Range": fmt.Sprintf("bytes %d-%d/%d", start, end, stat.Size()),
				}
			}
		}
	}

	pushURL := strings.TrimRight(cfg.MasterURL, "/") + "/api/receiver/stream-push/" + req.Token
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, pushURL, body)
	if err != nil {
		m.log.Warn("Failed to create push request for %s: %v", req.Token, err)
		return
	}
	httpReq.Header.Set("Content-Type", contentType)
	httpReq.Header.Set("X-API-Key", cfg.APIKey)
	httpReq.Header.Set("X-Stream-Status", strconv.Itoa(statusCode))
	httpReq.Header.Set("Accept-Ranges", "bytes")
	httpReq.ContentLength = contentLength
	for k, v := range extraHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := streamHTTPClient.Do(httpReq)
	if err != nil {
		if ctx.Err() == nil {
			m.log.Warn("Stream push failed for %s: %v", req.Token, err)
		}
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		m.log.Warn("Stream push %s returned HTTP %d", req.Token, resp.StatusCode)
	}
}

// isValidToken accepts only [A-Za-z0-9-]+ to keep the upstream URL path safe
// against injection. Master generates UUIDv4 tokens which trivially conform.
func isValidToken(token string) bool {
	if token == "" {
		return false
	}
	for _, ch := range token {
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= 'A' && ch <= 'Z':
		case ch >= '0' && ch <= '9':
		case ch == '-':
		default:
			return false
		}
	}
	return true
}

// parseRange parses an HTTP Range header value. Mirrors the slave's parseRange.
func parseRange(rangeHeader string, fileSize int64) (start, end int64, err error) {
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return 0, 0, fmt.Errorf("unsupported range format")
	}
	spec := strings.TrimPrefix(rangeHeader, "bytes=")
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range")
	}
	parseInt := func(s string) (int64, error) {
		return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	}
	switch {
	case parts[0] == "":
		n, perr := parseInt(parts[1])
		if perr != nil {
			return 0, 0, fmt.Errorf("invalid suffix range: %w", perr)
		}
		start = max(fileSize-n, 0)
		end = fileSize - 1
	case parts[1] == "":
		start, err = parseInt(parts[0])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid start: %w", err)
		}
		end = fileSize - 1
	default:
		start, err = parseInt(parts[0])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid start: %w", err)
		}
		end, err = parseInt(parts[1])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid end: %w", err)
		}
	}
	if start < 0 || start >= fileSize || end < start || end >= fileSize {
		return 0, 0, fmt.Errorf("range out of bounds")
	}
	return start, end, nil
}

// resolveAndValidate ensures path resolves to a regular file under one of the
// allowed roots. Symlinks are followed and the final destination must still
// be inside an allowed root, preventing escape via crafted symlinks.
func resolveAndValidate(reqPath string, allowed []string) (string, error) {
	cleaned := filepath.Clean(reqPath)
	if cleaned == "." || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("path traversal detected")
	}
	for _, dir := range allowed {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		absDir, err = filepath.EvalSymlinks(absDir)
		if err != nil {
			continue
		}

		var fullPath string
		if filepath.IsAbs(cleaned) {
			fullPath = filepath.Clean(cleaned)
		} else {
			fullPath = filepath.Join(absDir, cleaned)
		}
		absPath, err := filepath.Abs(fullPath)
		if err != nil {
			continue
		}
		absPath, err = filepath.EvalSymlinks(absPath)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(absDir, absPath)
		if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
			continue
		}
		if info, err := os.Stat(absPath); err == nil && info.Mode().IsRegular() {
			return absPath, nil
		}
	}
	return "", fmt.Errorf("file not found in allowed directories")
}

// relativizeUnderRoot returns (relPath, true) if absPath sits under any of the
// allowed roots. Used at catalog-build time to convert absolute media paths
// into the slave-relative form the master expects.
func relativizeUnderRoot(absPath string, roots []string) (string, bool) {
	resolved, err := filepath.Abs(absPath)
	if err != nil {
		return "", false
	}
	resolved = filepath.Clean(resolved)
	for _, root := range roots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		absRoot = filepath.Clean(absRoot)
		rel, err := filepath.Rel(absRoot, resolved)
		if err != nil || rel == "" || rel == "." {
			continue
		}
		// Reject anything that escaped the root (rel starts with "..")
		if strings.HasPrefix(rel, "..") {
			continue
		}
		// Use forward slashes on the wire for cross-platform consistency.
		// The master and other slaves will be running Linux even when this
		// follower is on Windows during dev/testing.
		return filepath.ToSlash(rel), true
	}
	return "", false
}

// contentTypeForName maps a filename's extension to a MIME type, falling back
// to application/octet-stream so the master always has a non-empty value.
func contentTypeForName(name string) string {
	return helpers.MediaContentType(name)
}

// deliverThumbnail handles a master's thumb_request by reading the local
// thumbnail file for remoteID under the configured thumbnails directory and
// POSTing it to the master's stream-push endpoint. The same delivery
// pipeline as full media files is reused so masters get a uniform flow.
//
// remoteID format is validated: only the slave's own item.IDs (UUIDs or
// fingerprints) are accepted, and the path is composed locally with
// filepath.Join under thumbnailDir. The master never names a filesystem
// path here so this cannot be coerced into reading arbitrary files.
func (m *Module) deliverThumbnail(ctx context.Context, cfg config.FollowerConfig, req thumbRequest) {
	if !isValidToken(req.Token) {
		m.log.Warn("Thumb request rejected: invalid token format %q", req.Token)
		return
	}
	if !isValidThumbID(req.RemoteID) {
		m.log.Warn("Thumb request rejected: invalid remote_id %q", req.RemoteID)
		return
	}

	thumbDir := strings.TrimSpace(m.config.Get().Directories.Thumbnails)
	if thumbDir == "" {
		m.log.Debug("Thumb request: no thumbnails directory configured on this server")
		return
	}

	// Try variants in order of preference. WebP is preferred when the master
	// hinted that the user accepts it. Master has no fallback to placeholder
	// here so we hand back the JPEG even when WebP is preferred but missing.
	candidates := []struct {
		name        string
		contentType string
	}{
		{req.RemoteID + ".jpg", "image/jpeg"},
	}
	if req.PreferWebP {
		candidates = append([]struct {
			name        string
			contentType string
		}{
			{req.RemoteID + ".webp", "image/webp"},
		}, candidates...)
	}

	var (
		absPath     string
		contentType string
	)
	for _, c := range candidates {
		p := filepath.Join(thumbDir, c.name)
		// Ensure resolved path stays under thumbDir even after symlink eval.
		// EvalSymlinks fails for non-existent files; check existence first.
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			continue
		}
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			continue
		}
		absDir, err := filepath.EvalSymlinks(thumbDir)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(absDir, resolved)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		absPath = resolved
		contentType = c.contentType
		break
	}

	if absPath == "" {
		m.log.Debug("Thumb request: no thumbnail file for remote_id=%s", req.RemoteID)
		return
	}

	file, err := os.Open(absPath)
	if err != nil {
		m.log.Warn("Failed to open thumbnail %s: %v", absPath, err)
		return
	}
	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil {
		m.log.Warn("Failed to stat thumbnail %s: %v", absPath, err)
		return
	}

	pushURL := strings.TrimRight(cfg.MasterURL, "/") + "/api/receiver/stream-push/" + req.Token
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, pushURL, file)
	if err != nil {
		m.log.Warn("Failed to build thumb push request for %s: %v", req.Token, err)
		return
	}
	httpReq.Header.Set("Content-Type", contentType)
	httpReq.Header.Set("X-API-Key", cfg.APIKey)
	httpReq.Header.Set("X-Stream-Status", strconv.Itoa(http.StatusOK))
	httpReq.Header.Set("Cache-Control", "public, max-age=86400")
	httpReq.ContentLength = stat.Size()

	resp, err := streamHTTPClient.Do(httpReq)
	if err != nil {
		if ctx.Err() == nil {
			m.log.Warn("Thumb push failed for %s: %v", req.Token, err)
		}
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		m.log.Warn("Thumb push %s returned HTTP %d", req.Token, resp.StatusCode)
	}
}

// isValidThumbID accepts only [A-Za-z0-9-] so the joined filepath cannot
// escape the thumbnails directory via "..", path separators, or null bytes.
// Slave-issued media IDs are UUIDs or hex strings which trivially conform.
func isValidThumbID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, ch := range id {
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= 'A' && ch <= 'Z':
		case ch >= '0' && ch <= '9':
		case ch == '-' || ch == '_':
		default:
			return false
		}
	}
	return true
}
