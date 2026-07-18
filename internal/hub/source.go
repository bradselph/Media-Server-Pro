package hub

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"media-server-pro/internal/logger"
)

// DownloadZip streams url to destPath, writing to a ".part" file first and
// renaming on success so an interrupted download never looks complete. If the
// destination already exists (non-empty) and force is false, the download is
// skipped and the existing archive reused — so a re-run after an interrupted
// import doesn't re-fetch the archive.
func DownloadZip(ctx context.Context, url, destPath string, force bool, log *logger.Logger) error {
	if !force {
		if fi, err := os.Stat(destPath); err == nil && fi.Size() > 0 {
			log.Info("Reusing existing archive %s (%s); pass -refresh to re-download", destPath, humanBytes(fi.Size()))
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "media-server-pro-hub-import")

	// No client-level timeout: the archive is large. Cancellation is via ctx
	// (Ctrl-C / SIGTERM), which aborts the in-flight read.
	log.Info("Downloading %s …", url)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: unexpected status %s", resp.Status)
	}

	tmp := destPath + ".part"
	out, err := os.Create(tmp) //nolint:gosec // operator-configured work dir
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	pr := &progressReader{r: resp.Body, total: resp.ContentLength, log: log, start: time.Now(), label: "downloaded"}
	if _, err := io.Copy(out, pr); err != nil { //nolint:gosec // streamed to disk, not memory
		_ = out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("download copy: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, destPath); err != nil {
		return fmt.Errorf("finalize download: %w", err)
	}
	log.Info("Downloaded archive to %s", destPath)
	return nil
}

// zippedCSV streams a single entry out of an open zip and closes both the entry
// reader and the archive together.
type zippedCSV struct {
	rc io.ReadCloser
	zr *zip.ReadCloser
}

func (z *zippedCSV) Read(p []byte) (int, error) { return z.rc.Read(p) }

func (z *zippedCSV) Close() error {
	err := z.rc.Close()
	if zErr := z.zr.Close(); zErr != nil && err == nil {
		err = zErr
	}
	return err
}

// OpenZippedCSV opens the catalog CSV entry inside zipPath as a streaming reader
// without extracting the (multi-GB) file to disk. It prefers the largest ".csv"
// entry, falling back to the single largest entry. The returned ReadCloser must
// be closed by the caller.
func OpenZippedCSV(zipPath string, log *logger.Logger) (io.ReadCloser, string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, "", fmt.Errorf("open zip %s: %w", zipPath, err)
	}
	var chosen *zip.File
	pick := func(f *zip.File) {
		if chosen == nil || f.UncompressedSize64 > chosen.UncompressedSize64 {
			chosen = f
		}
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(f.Name), ".csv") {
			pick(f)
		}
	}
	if chosen == nil { // no .csv entry — take the largest file
		for _, f := range zr.File {
			if !f.FileInfo().IsDir() {
				pick(f)
			}
		}
	}
	if chosen == nil {
		_ = zr.Close()
		return nil, "", fmt.Errorf("zip %s contains no files", zipPath)
	}
	rc, err := chosen.Open()
	if err != nil {
		_ = zr.Close()
		return nil, "", fmt.Errorf("open zip entry %q: %w", chosen.Name, err)
	}
	if log != nil {
		log.Info("Streaming %q (%s uncompressed) from archive", chosen.Name, humanBytes(int64(chosen.UncompressedSize64)))
	}
	return &zippedCSV{rc: rc, zr: zr}, chosen.Name, nil
}

// progressReader logs throughput roughly every 64 MiB as bytes flow through it.
type progressReader struct {
	r      io.Reader
	total  int64
	read   int64
	nextAt int64
	log    *logger.Logger
	start  time.Time
	label  string
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.read += int64(n)
	if p.log != nil && p.read >= p.nextAt {
		p.nextAt = p.read + 64*1024*1024
		if p.total > 0 {
			pct := float64(p.read) / float64(p.total) * 100
			p.log.Info("%s %s / %s (%.0f%%)", p.label, humanBytes(p.read), humanBytes(p.total), pct)
		} else {
			p.log.Info("%s %s", p.label, humanBytes(p.read))
		}
	}
	return n, err
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
