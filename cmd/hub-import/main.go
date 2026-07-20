// Command hub-import loads the BETA Hub embed catalog into the database from the
// server, decoupled from the running web server. It handles the large (multi-GB)
// catalog end-to-end and is meant to be run on the host — via SSH, `docker exec`,
// a systemd oneshot, or cron — not through the in-process admin-panel trigger.
//
// Two sources:
//   - a URL to a zipped catalog (-url / HUB_SOURCE_URL): the archive is
//     downloaded once, and the CSV entry is streamed straight out of the zip
//     into the DB — the multi-GB CSV is never written to disk.
//   - a local CSV file (-csv / hub.csv_path).
//
// It connects with the same config.json/env as the server, runs the schema
// migration (so hub_embeds exists), and writes in idempotent batches keyed on
// embed_id so re-running after an interruption is safe. A first import (empty
// table) INSERT IGNOREs; a re-import into a populated catalog upserts (add new +
// refresh changed rows) so an updated snapshot is applied incrementally without a
// destructive clear+reinsert. Force upsert on any run with -upsert.
//
// Usage:
//
//	hub-import [-config config.json]
//	           [-url URL | -csv PATH] [-work-dir DIR]
//	           [-batch-size N] [-limit N] [-truncate] [-upsert] [-dry-run]
//	           [-keep-zip] [-refresh]
//
// Examples:
//
//	hub-import                                  # source from config/env (URL or CSV)
//	hub-import -url https://host/catalog.zip    # download, unzip-stream, import
//	hub-import -csv /data/hub.csv               # local file; upserts if non-empty
//	hub-import -truncate                        # clear the table, then full re-import
//	hub-import -upsert                          # force refresh of existing rows
//	hub-import -limit 5000 -dry-run             # parse first 5k rows, write nothing
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/hub"
	"media-server-pro/internal/logger"
	mysqlrepo "media-server-pro/internal/repositories/mysql"
)

func main() {
	var (
		configPath = flag.String("config", "config.json", "Path to config file")
		csvPath    = flag.String("csv", "", "Local CSV path (overrides hub.csv_path)")
		urlFlag    = flag.String("url", "", "URL of a zipped catalog to download (overrides hub.source_url)")
		workDir    = flag.String("work-dir", "", "Scratch dir for the downloaded archive (overrides hub.work_dir)")
		batchSize  = flag.Int("batch-size", 0, "Rows per insert batch (0 = config/default)")
		limit      = flag.Int64("limit", 0, "Import at most N rows (0 = all; useful for testing)")
		truncate   = flag.Bool("truncate", false, "Clear the hub_embeds table before importing")
		upsertFlag = flag.Bool("upsert", false, "Refresh existing rows (INSERT ... ON DUPLICATE KEY UPDATE) instead of INSERT IGNORE; auto-enabled when the catalog is non-empty")
		dryRun     = flag.Bool("dry-run", false, "Parse and count only; do not write to the database")
		keepZip    = flag.Bool("keep-zip", false, "Keep the downloaded archive after import (default: delete)")
		refresh    = flag.Bool("refresh", false, "Re-download even if the archive already exists")
	)
	flag.Parse()

	log := logger.New("hub-import")
	defer logger.Shutdown()

	cfg := config.NewManager(*configPath)
	if err := cfg.Load(); err != nil {
		fatal(log, "failed to load config %q: %v", *configPath, err)
	}
	hubCfg := cfg.Get().Hub

	// Resolve the source. An explicit -csv forces local-file mode; otherwise a
	// URL (flag or config) selects download mode; otherwise fall back to a
	// configured local CSV path.
	url := *urlFlag
	if url == "" && *csvPath == "" {
		url = hubCfg.SourceURL
	}
	localPath := *csvPath
	if url == "" && localPath == "" {
		localPath = hubCfg.CSVPath
	}
	if url == "" && localPath == "" {
		fatal(log, "no source: set -url / HUB_SOURCE_URL / hub.source_url, or -csv / HUB_CSV_PATH / hub.csv_path")
	}

	// Cancel cleanly on Ctrl-C / SIGTERM — the streaming loop and the download
	// check ctx, so a partial (idempotent) run stops promptly and can be resumed.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start the database module: connects and runs migrations (creates hub_embeds
	// on first run), exactly like the server does on boot.
	db := database.NewModule(cfg)
	startCtx, cancelStart := context.WithTimeout(ctx, 60*time.Second)
	if err := db.Start(startCtx); err != nil {
		cancelStart()
		fatal(log, "database start failed: %v", err)
	}
	cancelStart()
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_ = db.Stop(stopCtx)
		cancel()
	}()

	gormDB := db.GORM()
	if gormDB == nil {
		fatal(log, "database connected but GORM handle is nil")
	}
	repo := mysqlrepo.NewHubEmbedRepository(gormDB)

	if *truncate && !*dryRun {
		log.Info("Truncating hub_embeds before import...")
		if err := repo.DeleteAll(ctx); err != nil {
			fatal(log, "truncate failed: %v", err)
		}
	}

	bs := *batchSize
	if bs <= 0 {
		bs = hubCfg.ImportBatchSize
	}
	// Upsert (refresh existing + add new, no duplicates, no full rewrite) when the
	// operator asks for it OR when the catalog already has rows — a re-import of an
	// updated snapshot. A -truncate above empties the table first, so that path
	// falls back to a plain insert with an exact new-row count. Skipped for dry-run.
	existing, _ := repo.CountAll(ctx)
	upsert := (*upsertFlag || existing > 0) && !*dryRun
	if upsert {
		log.Info("Catalog has %d rows — importing with upsert (add new + refresh changed) instead of INSERT IGNORE", existing)
	}
	opts := hub.ImportOptions{BatchSize: bs, Limit: *limit, DryRun: *dryRun, Upsert: upsert}

	var read, inserted int64
	var importErr error
	start := time.Now()

	if url != "" {
		dir := firstNonEmpty(*workDir, hubCfg.WorkDir, filepath.Join(os.TempDir(), "msp-hub-import"))
		zipPath := filepath.Join(dir, "hub-catalog.zip")
		if err := hub.DownloadZip(ctx, url, zipPath, *refresh, log); err != nil {
			importFail(log, ctx, start, 0, 0, err)
		}
		csvReader, name, err := hub.OpenZippedCSV(zipPath, log)
		if err != nil {
			fatal(log, "open archive: %v", err)
		}
		log.Info("Importing from archive entry %q (batch=%d, limit=%d, dry-run=%v)", name, bs, *limit, *dryRun)
		read, inserted, importErr = hub.ImportReader(ctx, csvReader, repo, log, opts)
		_ = csvReader.Close()
		if !*keepZip && importErr == nil && !*dryRun {
			if err := os.Remove(zipPath); err == nil {
				log.Info("Removed downloaded archive %s", zipPath)
			}
		}
	} else {
		if fi, err := os.Stat(localPath); err != nil {
			fatal(log, "CSV path not accessible: %v", err)
		} else if fi.IsDir() {
			fatal(log, "CSV path is a directory, expected a file: %s", localPath)
		}
		log.Info("Importing from %s (batch=%d, limit=%d, dry-run=%v)", localPath, bs, *limit, *dryRun)
		read, inserted, importErr = hub.ImportCSVWithOptions(ctx, localPath, repo, log, opts)
	}

	if importErr != nil {
		importFail(log, ctx, start, read, inserted, importErr)
	}

	total, _ := repo.CountAll(context.Background())
	// With upsert, `inserted` is the driver's affected-row count (insert=1,
	// update=2), not a pure new-row count — label it accordingly.
	writeLabel := "newly inserted"
	if upsert {
		writeLabel = "rows written (new + refreshed)"
	}
	log.Info("Import finished in %s: %d rows read, %d %s, %d total in catalog",
		time.Since(start).Round(time.Second), read, inserted, writeLabel, total)
}

// importFail handles a failed/cancelled import: a clean 130 on cancellation
// (resumable), otherwise a fatal error.
func importFail(log *logger.Logger, ctx context.Context, start time.Time, read, inserted int64, err error) {
	elapsed := time.Since(start).Round(time.Second)
	// "written" (not "inserted") because the upsert path reports affected rows, not
	// a pure new-row count — same reason the success line is relabeled for upsert.
	if ctx.Err() != nil {
		log.Warn("Cancelled after %s: %d read, %d written (re-run to resume — safe/idempotent)", elapsed, read, inserted)
		logger.Shutdown()
		os.Exit(130)
	}
	fatal(log, "import failed after %s (%d read, %d written): %v", elapsed, read, inserted, err)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func fatal(log *logger.Logger, format string, args ...any) {
	log.Error(format, args...)
	logger.Shutdown()
	_, _ = fmt.Fprintln(os.Stderr, "hub-import failed")
	os.Exit(1)
}
