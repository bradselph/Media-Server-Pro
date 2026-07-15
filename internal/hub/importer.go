package hub

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
)

// embedIDRe extracts the provider embed id from the iframe HTML in CSV field 1,
// e.g. `<iframe src="https://www.pornhub.com/embed/abc123" ...>` -> "abc123".
var embedIDRe = regexp.MustCompile(`/embed/([A-Za-z0-9]+)`)

// maxImportLineBytes is the per-line ceiling for the CSV scanner. Lines carry
// long iframe HTML plus many ';'-separated preview URLs, so the default 64KiB
// bufio.Scanner token limit is far too small and would silently truncate.
const maxImportLineBytes = 8 * 1024 * 1024 // 8 MiB

// TriggerImport starts a background import driven by the config knobs. Source
// resolution: an explicit pathOverride wins; else hub.source_url (downloaded and
// stream-extracted); else hub.csv_path (local file). Only one import runs at a
// time. This is what the admin "Start import" button and auto-import both call.
func (m *Module) TriggerImport(pathOverride string) error {
	if !m.config.Get().Hub.Enabled {
		return errors.New("hub feature is disabled")
	}
	if m.repo == nil {
		return errors.New("hub module is not initialized (database unavailable)")
	}
	hubCfg := m.config.Get().Hub

	url, path := "", pathOverride
	if path == "" {
		if hubCfg.SourceURL != "" {
			url = hubCfg.SourceURL
		} else {
			path = hubCfg.CSVPath
		}
	}
	switch {
	case url != "":
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			return errors.New("hub.source_url must be an http(s) URL")
		}
	case path != "":
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("csv path not accessible: %w", err)
		}
	default:
		return errors.New("no source configured (set hub.source_url or hub.csv_path)")
	}

	m.importMu.Lock()
	if m.importState.Running {
		m.importMu.Unlock()
		return errors.New("an import is already running")
	}
	source := path
	if url != "" {
		source = url
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.importCancel = cancel
	m.importState = ImportState{Running: true, Source: source, Path: path, StartedAt: time.Now()}
	batchSize := hubCfg.ImportBatchSize
	workDir := hubCfg.WorkDir
	m.importMu.Unlock()

	go func() {
		defer cancel()
		err := m.runImport(ctx, url, path, workDir, batchSize)
		m.importMu.Lock()
		m.importState.Running = false
		m.importState.Phase = ""
		m.importState.FinishedAt = time.Now()
		if err != nil {
			m.importState.Error = err.Error()
		}
		m.importCancel = nil
		m.importMu.Unlock()
		if err == nil {
			m.catMu.Lock()
			m.catCache = nil // force facet recompute against the new data
			m.catMu.Unlock()
		}
	}()
	return nil
}

// runImport executes the resolved import (download+stream-extract, or local file).
func (m *Module) runImport(ctx context.Context, url, path, workDir string, batchSize int) error {
	progress := func(read, inserted int64) {
		m.importMu.Lock()
		m.importState.RowsRead = read
		m.importState.Inserted = inserted
		m.importMu.Unlock()
	}
	opts := ImportOptions{BatchSize: batchSize, Progress: progress}

	if url != "" {
		if workDir == "" {
			workDir = filepath.Join(os.TempDir(), "msp-hub-import")
		}
		zipPath := filepath.Join(workDir, "hub-catalog.zip")
		m.setPhase("downloading")
		if err := DownloadZip(ctx, url, zipPath, false, m.log); err != nil {
			return err
		}
		m.setPhase("importing")
		rc, name, err := OpenZippedCSV(zipPath, m.log)
		if err != nil {
			return err
		}
		m.log.Info("Hub: importing archive entry %q", name)
		read, inserted, err := ImportReader(ctx, rc, m.repo, m.log, opts)
		_ = rc.Close()
		m.setCounts(read, inserted)
		if err != nil {
			return err
		}
		if rmErr := os.Remove(zipPath); rmErr == nil {
			m.log.Info("Hub: removed downloaded archive %s", zipPath)
		}
		return nil
	}

	m.setPhase("importing")
	read, inserted, err := ImportCSVWithOptions(ctx, path, m.repo, m.log, opts)
	m.setCounts(read, inserted)
	return err
}

func (m *Module) setPhase(p string) {
	m.importMu.Lock()
	m.importState.Phase = p
	m.importMu.Unlock()
}

func (m *Module) setCounts(read, inserted int64) {
	m.importMu.Lock()
	m.importState.RowsRead = read
	m.importState.Inserted = inserted
	m.importMu.Unlock()
}

// maybeAutoImport bootstraps the catalog on start when hub.auto_import is set and
// the table is empty. Best-effort: logs and returns on any error.
func (m *Module) maybeAutoImport() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	n, err := m.repo.CountAll(ctx)
	cancel()
	if err != nil {
		m.log.Warn("Hub auto-import: row count failed, skipping: %v", err)
		return
	}
	if n > 0 {
		m.log.Info("Hub auto-import: catalog already has %d rows, skipping", n)
		return
	}
	m.log.Info("Hub auto-import: empty catalog — starting import")
	if err := m.TriggerImport(""); err != nil {
		m.log.Warn("Hub auto-import: %v", err)
	}
}

// ImportStatus returns a snapshot of the import job plus the current row count.
func (m *Module) ImportStatus() ImportState {
	m.importMu.Lock()
	st := m.importState
	m.importMu.Unlock()
	if repo := m.ready(); repo != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if n, err := repo.CountAll(ctx); err == nil {
			st.TotalRows = n
		}
		cancel()
	}
	return st
}

// ImportOptions tunes a CSV import run. Used by the standalone CLI importer
// (cmd/hub-import) for one-off, server-side processing of the catalog file.
type ImportOptions struct {
	BatchSize int                        // rows per insert (<=0 => default 2000)
	Limit     int64                      // stop after N valid rows (0 => all)
	DryRun    bool                       // parse + count only; never write to the DB
	Progress  func(read, inserted int64) // optional, called after each flushed batch
}

// ImportCSV streams a pipe-delimited catalog file into the repository in batches.
// It is cancellable via ctx and reports progress via the optional callback.
// Returns (rowsRead, rowsInserted, error). Thin wrapper over ImportCSVWithOptions
// kept for the in-process (admin-triggered) import path.
func ImportCSV(ctx context.Context, path string, repo repositories.HubEmbedRepository, batchSize int, log *logger.Logger, progress func(read, inserted int64)) (int64, int64, error) {
	return ImportCSVWithOptions(ctx, path, repo, log, ImportOptions{BatchSize: batchSize, Progress: progress})
}

// ImportCSVWithOptions opens the CSV file at path and streams it into the
// repository. Thin wrapper over ImportReader for the file-path case.
func ImportCSVWithOptions(ctx context.Context, path string, repo repositories.HubEmbedRepository, log *logger.Logger, opts ImportOptions) (int64, int64, error) {
	f, err := os.Open(path) //nolint:gosec // operator-provided catalog path (deny-listed from the live admin API)
	if err != nil {
		return 0, 0, fmt.Errorf("open hub csv: %w", err)
	}
	defer func() { _ = f.Close() }()
	return ImportReader(ctx, f, repo, log, opts)
}

// ImportReader is the core streaming importer over any reader — a local file, or
// a CSV entry streamed straight out of a downloaded zip (no 18 GB temp file). It
// uses an enlarged scanner buffer (lines carry long iframe HTML + many preview
// URLs), inserts in idempotent batches (INSERT IGNORE on embed_id), honors ctx
// cancellation between batches, and supports Limit (partial import) and DryRun.
func ImportReader(ctx context.Context, r io.Reader, repo repositories.HubEmbedRepository, log *logger.Logger, opts ImportOptions) (int64, int64, error) {
	batchSize := opts.BatchSize
	if batchSize <= 0 || batchSize > 5000 {
		batchSize = 2000
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxImportLineBytes)

	batch := make([]*repositories.HubEmbedRecord, 0, batchSize)
	var read, inserted int64

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if !opts.DryRun {
			n, err := repo.BatchInsert(ctx, batch)
			if err != nil {
				return err
			}
			inserted += n
		}
		batch = batch[:0]
		return nil
	}

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return read, inserted, err // cancelled — stop reading the huge file promptly
		}
		rec := parseLine(scanner.Text())
		if rec == nil {
			continue // malformed / no embed id — skip
		}
		batch = append(batch, rec)
		read++
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return read, inserted, fmt.Errorf("hub csv batch insert at row %d: %w", read, err)
			}
			if opts.Progress != nil {
				opts.Progress(read, inserted)
			}
		}
		if read%100000 == 0 && log != nil {
			log.Info("hub import: %d rows read, %d inserted", read, inserted)
		}
		if opts.Limit > 0 && read >= opts.Limit {
			break // partial import (testing / staged loads)
		}
	}
	if err := flush(); err != nil {
		return read, inserted, fmt.Errorf("hub csv final batch insert: %w", err)
	}
	if err := scanner.Err(); err != nil {
		return read, inserted, fmt.Errorf("hub csv scan error after %d rows: %w", read, err)
	}
	if opts.Progress != nil {
		opts.Progress(read, inserted)
	}
	if log != nil {
		verb := "complete"
		if opts.DryRun {
			verb = "dry-run complete"
		}
		log.Info("hub import %s: %d rows read, %d inserted", verb, read, inserted)
	}
	return read, inserted, nil
}

// parseLine parses one pipe-delimited CSV row into a record, or nil if invalid.
// Fields: 0=iframe html, 1=thumb, 2=';'preview thumbs, 3=title, 4=';'tags,
// 5=';'categories, 6=pornstar, 7=duration, 8=views, 9=rating_up, 10=rating_down.
func parseLine(line string) *repositories.HubEmbedRecord {
	fields := strings.Split(line, "|")
	if len(fields) < 11 {
		return nil
	}
	m := embedIDRe.FindStringSubmatch(fields[0])
	if m == nil {
		return nil
	}
	return &repositories.HubEmbedRecord{
		EmbedID:      truncate(m[1], 64),
		ThumbURL:     strings.TrimSpace(fields[1]),
		PreviewURLs:  capPreviews(fields[2]),
		Title:        truncate(strings.TrimSpace(fields[3]), 500),
		Tags:         strings.TrimSpace(fields[4]),
		Categories:   strings.TrimSpace(fields[5]),
		Pornstar:     truncate(strings.TrimSpace(fields[6]), 500),
		DurationSecs: atoiSafe(fields[7]),
		Views:        atoi64Safe(fields[8]),
		RatingUp:     atoiSafe(fields[9]),
		RatingDown:   atoiSafe(fields[10]),
	}
}

// capPreviews keeps at most maxPreviewURLs preview URLs to bound row size.
func capPreviews(s string) string {
	parts := strings.Split(s, ";")
	if len(parts) <= maxPreviewURLs {
		return strings.TrimSpace(s)
	}
	return strings.Join(parts[:maxPreviewURLs], ";")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func atoiSafe(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func atoi64Safe(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}
