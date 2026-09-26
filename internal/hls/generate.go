package hls

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"media-server-pro/internal/config"
	"media-server-pro/pkg/models"
)

// GenerateHLSParams holds parameters for starting HLS transcoding.
type GenerateHLSParams struct {
	MediaPath string
	MediaID   string
	Qualities []string
	// HighPriority marks this as a live, user-triggered request (e.g. a viewer's
	// player) rather than background pre-generation, so its transcode goroutine
	// competes for a slot ahead of low-priority callers — see tryAcquireTranscode.
	HighPriority bool
}

// GenerateHLS starts HLS transcoding for a media file.
// The mediaID (stable UUID) is used as the job ID so that HLS cache survives file moves/renames.
func (m *Module) GenerateHLS(ctx context.Context, params *GenerateHLSParams) (*models.HLSJob, error) {
	if params == nil {
		return nil, fmt.Errorf("GenerateHLSParams cannot be nil")
	}
	if err := m.checkGenerateHLSPrereqs(params.MediaPath); err != nil {
		return nil, err
	}
	jobID := params.MediaID
	outputDir := filepath.Join(m.cacheDir, jobID)
	resolved := m.resolveHLSQualities(ctx, &resolveQualitiesParams{MediaPath: params.MediaPath, Qualities: params.Qualities})

	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()
	return m.createOrReuseHLSJobLocked(&createOrReuseHLSJobParams{
		Ctx:          ctx,
		JobID:        jobID,
		MediaPath:    params.MediaPath,
		OutputDir:    outputDir,
		Qualities:    resolved,
		HighPriority: params.HighPriority,
	})
}

// resolveQualitiesParams holds arguments for resolving/filtering HLS quality lists.
type resolveQualitiesParams struct {
	MediaPath string
	Qualities []string
}

// checkGenerateHLSPrereqs verifies HLS is available and the media file exists.
func (m *Module) checkGenerateHLSPrereqs(mediaPath string) error {
	if !m.IsAvailable() {
		if m.ffmpegPath == "" {
			return fmt.Errorf("HLS transcoding unavailable: ffmpeg not found. Use direct streaming instead")
		}
		return fmt.Errorf("HLS transcoding is disabled in server configuration")
	}
	// Only stat-check absolute local filesystem paths. S3 object keys such as
	// "videos/foo.mp4" are not absolute and cannot be checked with os.Stat;
	// ffmpeg will report the error directly when transcoding starts.
	if filepath.IsAbs(mediaPath) {
		if _, err := os.Stat(mediaPath); err != nil {
			return fmt.Errorf("media file not found: %w", err)
		}
	}
	return nil
}

// defaultQualitiesFromConfig returns enabled quality names from config when none are specified.
func (m *Module) defaultQualitiesFromConfig(qualities []string) []string {
	if len(qualities) > 0 {
		return qualities
	}
	cfg := m.config.Get()
	out := make([]string, 0, len(cfg.HLS.QualityProfiles))
	for _, qp := range cfg.HLS.QualityProfiles {
		if qp.Enabled {
			out = append(out, qp.Name)
		}
	}
	return out
}

// filterQualitiesBySourceHeight keeps only qualities that do not exceed source height; logs when some are skipped.
func (m *Module) filterQualitiesBySourceHeight(ctx context.Context, p *resolveQualitiesParams) []string {
	if p == nil {
		return nil
	}
	sourceHeight := m.getSourceHeight(ctx, p.MediaPath)
	if sourceHeight <= 0 {
		return p.Qualities
	}
	filtered := make([]string, 0, len(p.Qualities))
	known := make([]string, 0, len(p.Qualities))
	for _, q := range p.Qualities {
		profile := m.getQualityProfile(q)
		if profile == nil {
			// Unknown quality name: there is no profile to transcode with, and
			// admitting it (the old `profile == nil || ...`) produced a variant
			// that never completed, wedging the job permanently at "Running".
			m.log.Warn("Skipping unknown HLS quality %q (no matching profile) for %s", q, filepath.Base(p.MediaPath))
			continue
		}
		known = append(known, q)
		if profile.Height <= sourceHeight {
			filtered = append(filtered, q)
		}
	}
	if len(filtered) == 0 {
		// No known quality fits under the source height (e.g. a small source):
		// generate the known set rather than upscale-skipping everything. Only
		// fall back to the raw list if nothing was recognised at all.
		if len(known) > 0 {
			return known
		}
		return p.Qualities
	}
	if len(filtered) < len(p.Qualities) {
		m.log.Info("Source %s is %dpx tall — skipping upscale qualities, generating: %v",
			filepath.Base(p.MediaPath), sourceHeight, filtered)
	}
	return filtered
}

// resolveHLSQualities returns default qualities if none specified, filtered by source height.
func (m *Module) resolveHLSQualities(ctx context.Context, p *resolveQualitiesParams) []string {
	if p == nil {
		return nil
	}
	p.Qualities = m.defaultQualitiesFromConfig(p.Qualities)
	return m.filterQualitiesBySourceHeight(ctx, p)
}

// tryResolveExistingJob returns an existing job if it is valid and usable.
// If the job is completed but master.m3u8 is missing, the job is invalidated and (nil, false) is returned.
// Check and optional delete are done under a single lock to avoid TOCTOU with CreateOrReuseHLSJob.
func (m *Module) tryResolveExistingJob(mediaID string) (*models.HLSJob, bool) {
	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()
	job, ok := m.jobs[mediaID]
	if !ok {
		return nil, false
	}
	if job.Status != models.HLSStatusCompleted {
		return job, true
	}
	masterPath := filepath.Join(job.OutputDir, masterPlaylistName)
	if _, statErr := os.Stat(masterPath); statErr == nil {
		return job, true
	}
	m.log.Warn("HLS job %s marked complete but master.m3u8 missing from disk, will regenerate", job.ID)
	delete(m.jobs, job.ID)
	return nil, false
}

// CheckOrGenerateHLSParams holds parameters for checking or auto-generating HLS.
type CheckOrGenerateHLSParams struct {
	MediaPath string
	MediaID   string
	// HighPriority marks this as a live, user-triggered request — see
	// GenerateHLSParams.HighPriority.
	HighPriority bool
}

// CheckOrGenerateHLS checks if HLS exists for media path, auto-generates if configured.
func (m *Module) CheckOrGenerateHLS(ctx context.Context, params *CheckOrGenerateHLSParams) (*models.HLSJob, error) {
	if params == nil {
		return nil, fmt.Errorf("CheckOrGenerateHLSParams cannot be nil")
	}
	if job, ok := m.tryResolveExistingJob(params.MediaID); ok {
		// A background pre-generation cycle may have already queued this item at
		// low priority; a viewer requesting it now should not wait behind the
		// rest of that batch, so promote the still-pending job in place.
		if params.HighPriority && job.Status == models.HLSStatusPending {
			m.upgradeJobPriority(job.ID)
		}
		return job, nil
	}
	cfg := m.config.Get()
	if !cfg.HLS.AutoGenerate {
		return nil, fmt.Errorf("HLS not available and auto-generation is disabled")
	}
	m.log.Info("Auto-generating HLS for: %s", params.MediaPath)
	job, err := m.GenerateHLS(ctx, &GenerateHLSParams{MediaPath: params.MediaPath, MediaID: params.MediaID, Qualities: nil, HighPriority: params.HighPriority})
	if err != nil {
		return nil, fmt.Errorf("failed to start HLS generation: %w", err)
	}
	return job, nil
}

// getQualityProfile returns the HLS quality profile by name from config.
func (m *Module) getQualityProfile(name string) *config.HLSQuality {
	cfg := m.config.Get()
	for _, profile := range cfg.HLS.QualityProfiles {
		if profile.Name == name {
			return &profile
		}
	}
	return nil
}

// writePlaylistLineOpts holds arguments for writePlaylistLine to avoid string-heavy parameters.
type writePlaylistLineOpts struct {
	MasterPath string
	WrapMsg    string
}

// writePlaylistLine runs writeFn(); on error removes masterPath and returns a wrapped error.
func (m *Module) writePlaylistLine(opts *writePlaylistLineOpts, writeFn func() error) error {
	if opts == nil {
		return fmt.Errorf("writePlaylistLineOpts cannot be nil")
	}
	if err := writeFn(); err != nil {
		if removeErr := os.Remove(opts.MasterPath); removeErr != nil {
			m.log.Warn("Failed to remove corrupted playlist %s: %v", opts.MasterPath, removeErr)
		}
		return fmt.Errorf("%s: %w", opts.WrapMsg, err)
	}
	return nil
}

// writeVariantEntryOpts holds path and variant name for writeVariantEntry.
type writeVariantEntryOpts struct {
	MasterPath string
	Variant    string
}

// writeVariantEntry writes one variant's stream info and playlist path to the master playlist file.
func (m *Module) writeVariantEntry(file *os.File, opts *writeVariantEntryOpts, profile *config.HLSQuality) error {
	if opts == nil {
		return fmt.Errorf("writeVariantEntryOpts cannot be nil")
	}
	if profile == nil {
		return fmt.Errorf("HLSQuality profile cannot be nil")
	}
	if err := m.writePlaylistLine(&writePlaylistLineOpts{MasterPath: opts.MasterPath, WrapMsg: "failed to write stream info"}, func() error {
		_, err := fmt.Fprintf(file, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d,NAME=\"%s\"\n",
			profile.Bitrate+profile.AudioBitrate, profile.Width, profile.Height, opts.Variant)
		return err
	}); err != nil {
		return err
	}
	return m.writePlaylistLine(&writePlaylistLineOpts{MasterPath: opts.MasterPath, WrapMsg: "failed to write variant path"}, func() error {
		_, err := fmt.Fprintf(file, "%s/playlist.m3u8\n", opts.Variant)
		return err
	})
}

// generateMasterPlaylistParams holds arguments for generateMasterPlaylist.
type generateMasterPlaylistParams struct {
	OutputDir string
	Variants  []string
}

// generateMasterPlaylist creates (or atomically replaces) the master HLS
// playlist in outputDir for the given variants. transcode() calls this after
// every quality finishes — not just once at the very end — so a stream
// becomes playable as soon as the first quality is done (see
// publishMasterPlaylist/markJobPlayable). The content is written to a
// temporary sibling file first and renamed into place, so a reader (a player
// request, or discoverExistingJobs/validateExistingHLS on restart) never
// observes a half-written master.m3u8, and a failed write leaves whatever
// master.m3u8 already existed completely untouched.
func (m *Module) generateMasterPlaylist(p *generateMasterPlaylistParams) error {
	if p == nil {
		return fmt.Errorf("generateMasterPlaylistParams cannot be nil")
	}
	masterPath := filepath.Join(p.OutputDir, masterPlaylistName)
	file, err := os.CreateTemp(p.OutputDir, masterPlaylistName+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp master playlist: %w", err)
	}
	tmpPath := file.Name()
	// os.CreateTemp always uses mode 0600 regardless of umask; restore the
	// world-readable mode the previous os.Create-based version produced (these
	// files are served directly to players, like the 0o755 variant dirs in
	// prepareVariantDir).
	if chmodErr := os.Chmod(tmpPath, 0o644); chmodErr != nil { //nolint:gosec // G302: HLS playlists need world-read for serving
		m.log.Warn("Failed to set master playlist permissions for %s: %v", tmpPath, chmodErr)
	}
	// published tracks whether the rename below ran. The deferred cleanup is a
	// backstop for every other return path (a content-write or Sync/Close
	// error returns before reaching the rename): it closes the file handle —
	// ignoring the "already closed" error the success path's explicit Close
	// below leaves behind — and removes the temp file so a failed write never
	// leaves cache-dir litter or an unpublished master pointing nowhere.
	published := false
	defer func() {
		_ = file.Close()
		if !published {
			if removeErr := os.Remove(tmpPath); removeErr != nil && !os.IsNotExist(removeErr) {
				m.log.Warn("Failed to remove temp master playlist %s: %v", tmpPath, removeErr)
			}
		}
	}()

	plOpts := &writePlaylistLineOpts{MasterPath: tmpPath, WrapMsg: "failed to write playlist header"}
	if err := m.writePlaylistLine(plOpts, func() error {
		_, err := fmt.Fprintln(file, "#EXTM3U")
		return err
	}); err != nil {
		return err
	}
	plOpts.WrapMsg = "failed to write playlist version"
	if err := m.writePlaylistLine(plOpts, func() error {
		_, err := fmt.Fprintln(file, "#EXT-X-VERSION:3")
		return err
	}); err != nil {
		return err
	}

	for _, variant := range p.Variants {
		profile := m.getQualityProfile(variant)
		if profile == nil {
			continue
		}
		if err := m.writeVariantEntry(file, &writeVariantEntryOpts{MasterPath: tmpPath, Variant: variant}, profile); err != nil {
			return err
		}
	}

	// Sync + Close finalize the temp file's content before it is renamed into
	// place — a failure here means the content may be incomplete on disk, so
	// surface it instead of publishing a possibly truncated file over the real
	// master.m3u8.
	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync master playlist file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close master playlist file: %w", err)
	}

	if err := os.Rename(tmpPath, masterPath); err != nil {
		return fmt.Errorf("failed to publish master playlist: %w", err)
	}
	published = true
	return nil
}
