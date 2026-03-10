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
}

// GenerateHLS starts HLS transcoding for a media file.
// The mediaID (stable UUID) is used as the job ID so that HLS cache survives file moves/renames.
func (m *Module) GenerateHLS(ctx context.Context, params *GenerateHLSParams) (*models.HLSJob, error) {
	if err := m.checkGenerateHLSPrereqs(params.MediaPath); err != nil {
		return nil, err
	}
	jobID := params.MediaID
	outputDir := filepath.Join(m.cacheDir, jobID)
	resolved := m.resolveHLSQualities(ctx, &resolveQualitiesParams{MediaPath: params.MediaPath, Qualities: params.Qualities})

	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()
	return m.createOrReuseHLSJobLocked(&createOrReuseHLSJobParams{
		Ctx:       ctx,
		JobID:     jobID,
		MediaPath: params.MediaPath,
		OutputDir: outputDir,
		Qualities: resolved,
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
	if _, err := os.Stat(mediaPath); err != nil {
		return fmt.Errorf("media file not found: %w", err)
	}
	return nil
}

// defaultQualitiesFromConfig returns quality names from config when none are specified.
func (m *Module) defaultQualitiesFromConfig(qualities []string) []string {
	if len(qualities) > 0 {
		return qualities
	}
	cfg := m.config.Get()
	out := make([]string, 0, len(cfg.HLS.QualityProfiles))
	for _, qp := range cfg.HLS.QualityProfiles {
		out = append(out, qp.Name)
	}
	return out
}

// filterQualitiesBySourceHeight keeps only qualities that do not exceed source height; logs when some are skipped.
func (m *Module) filterQualitiesBySourceHeight(ctx context.Context, p *resolveQualitiesParams) []string {
	sourceHeight := m.getSourceHeight(ctx, p.MediaPath)
	if sourceHeight <= 0 {
		return p.Qualities
	}
	filtered := make([]string, 0, len(p.Qualities))
	for _, q := range p.Qualities {
		profile := m.getQualityProfile(q)
		if profile == nil || profile.Height <= sourceHeight {
			filtered = append(filtered, q)
		}
	}
	if len(filtered) == 0 {
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
	p.Qualities = m.defaultQualitiesFromConfig(p.Qualities)
	return m.filterQualitiesBySourceHeight(ctx, p)
}

// tryResolveExistingJob returns an existing job if it is valid and usable.
// If the job is completed but master.m3u8 is missing, the job is invalidated and (nil, false) is returned.
// Second return is true when the caller should use the returned job.
func (m *Module) tryResolveExistingJob(mediaID string) (*models.HLSJob, bool) {
	job, err := m.GetJobStatus(mediaID)
	if err != nil {
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
	m.jobsMu.Lock()
	delete(m.jobs, job.ID)
	m.jobsMu.Unlock()
	return nil, false
}

// CheckOrGenerateHLSParams holds parameters for checking or auto-generating HLS.
type CheckOrGenerateHLSParams struct {
	MediaPath string
	MediaID   string
}

// CheckOrGenerateHLS checks if HLS exists for media path, auto-generates if configured.
func (m *Module) CheckOrGenerateHLS(ctx context.Context, params *CheckOrGenerateHLSParams) (*models.HLSJob, error) {
	if job, ok := m.tryResolveExistingJob(params.MediaID); ok {
		return job, nil
	}
	cfg := m.config.Get()
	if !cfg.HLS.AutoGenerate {
		return nil, fmt.Errorf("HLS not available and auto-generation is disabled")
	}
	m.log.Info("Auto-generating HLS for: %s", params.MediaPath)
	job, err := m.GenerateHLS(ctx, &GenerateHLSParams{MediaPath: params.MediaPath, MediaID: params.MediaID, Qualities: nil})
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

// generateMasterPlaylist creates the master HLS playlist in outputDir for the given variants.
func (m *Module) generateMasterPlaylist(outputDir string, variants []string) error {
	masterPath := filepath.Join(outputDir, masterPlaylistName)
	file, err := os.Create(masterPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			m.log.Warn("Failed to close master playlist file: %v", err)
		}
	}()

	if _, err := fmt.Fprintln(file, "#EXTM3U"); err != nil {
		if removeErr := os.Remove(masterPath); removeErr != nil {
			m.log.Warn("Failed to remove corrupted playlist %s: %v", masterPath, removeErr)
		}
		return fmt.Errorf("failed to write playlist header: %w", err)
	}
	if _, err := fmt.Fprintln(file, "#EXT-X-VERSION:3"); err != nil {
		if removeErr := os.Remove(masterPath); removeErr != nil {
			m.log.Warn("Failed to remove corrupted playlist %s: %v", masterPath, removeErr)
		}
		return fmt.Errorf("failed to write playlist version: %w", err)
	}

	for _, variant := range variants {
		profile := m.getQualityProfile(variant)
		if profile == nil {
			continue
		}

		if _, err := fmt.Fprintf(file, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d,NAME=\"%s\"\n",
			profile.Bitrate+profile.AudioBitrate,
			profile.Width,
			profile.Height,
			variant,
		); err != nil {
			if removeErr := os.Remove(masterPath); removeErr != nil {
				m.log.Warn("Failed to remove corrupted playlist %s: %v", masterPath, removeErr)
			}
			return fmt.Errorf("failed to write stream info: %w", err)
		}
		if _, err := fmt.Fprintf(file, "%s/playlist.m3u8\n", variant); err != nil {
			if removeErr := os.Remove(masterPath); removeErr != nil {
				m.log.Warn("Failed to remove corrupted playlist %s: %v", masterPath, removeErr)
			}
			return fmt.Errorf("failed to write variant path: %w", err)
		}
	}

	return nil
}
