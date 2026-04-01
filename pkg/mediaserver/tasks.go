package mediaserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"media-server-pro/internal/backup"
	"media-server-pro/internal/hls"
	"media-server-pro/internal/media"
	"media-server-pro/internal/scanner"
	"media-server-pro/internal/suggestions"
	"media-server-pro/internal/tasks"
	"media-server-pro/internal/thumbnails"
)

const auditLogRetentionDays = 90

// registerTasks registers all periodic background tasks with the scheduler.
func (s *Server) registerTasks() {
	if s.Tasks == nil {
		return
	}

	scheduler := s.Tasks

	if s.Media != nil {
		s.registerMediaTasks(scheduler)
	}
	if s.Thumbnails != nil && s.Media != nil {
		s.registerThumbnailTask(scheduler)
	}
	if s.Auth != nil {
		scheduler.RegisterTask(tasks.TaskRegistration{
			ID: "session-cleanup", Name: "Session Cleanup",
			Description: "Removes expired user sessions from the database",
			Schedule:    1 * time.Hour,
			Func: func(ctx context.Context) error {
				return s.Auth.CleanupExpiredSessions(ctx)
			},
		})
	}
	if s.Backup != nil {
		s.registerBackupTask(scheduler)
	}
	if s.Scanner != nil && s.Media != nil {
		s.registerScannerTasks(scheduler)
	}
	if s.Duplicates != nil {
		scheduler.RegisterTask(tasks.TaskRegistration{
			ID: "duplicate-scan", Name: "Duplicate Media Scan",
			Description: "Scans local media library for files sharing the same content fingerprint",
			Schedule:    24 * time.Hour,
			Func: func(ctx context.Context) error {
				return s.Duplicates.ScanLocalMedia(ctx)
			},
		})
	}
	if s.Admin != nil {
		scheduler.RegisterTask(tasks.TaskRegistration{
			ID: "audit-log-cleanup", Name: "Audit Log Cleanup",
			Description: "Removes audit log entries older than the retention period",
			Schedule:    24 * time.Hour,
			Func: func(ctx context.Context) error {
				return s.Admin.CleanupAuditLogOlderThan(ctx, auditLogRetentionDays)
			},
		})
	}
	s.registerHealthCheck(scheduler)
	if s.HLS != nil && s.Media != nil {
		s.registerHLSTask(scheduler)
	}
}

func (s *Server) registerMediaTasks(scheduler *tasks.Module) {
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID: "media-scan", Name: "Media Library Scan",
		Description: "Scans configured directories for new and removed media files",
		Schedule:    1 * time.Hour,
		Func: func(ctx context.Context) error {
			if err := s.Media.Scan(); err != nil {
				return err
			}
			if s.Suggestions != nil {
				s.feedSuggestions()
			}
			return nil
		},
	})
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID: "metadata-cleanup", Name: "Metadata Cleanup",
		Description: "Removes metadata entries for media files that no longer exist on disk",
		Schedule:    24 * time.Hour,
		Func: func(ctx context.Context) error {
			return s.Media.Scan()
		},
	})
}

func (s *Server) feedSuggestions() {
	items := s.Media.ListMedia(media.Filter{})
	mediaInfos := make([]*suggestions.MediaInfo, 0, len(items))
	for _, item := range items {
		mediaInfos = append(mediaInfos, &suggestions.MediaInfo{
			Path: item.Path, StableID: item.ID, Title: item.Name,
			Category: item.Category, MediaType: string(item.Type),
			Tags: item.Tags, Views: item.Views, AddedAt: item.DateAdded,
			IsMature: item.IsMature,
		})
	}
	s.Suggestions.UpdateMediaData(mediaInfos)
}

func (s *Server) registerThumbnailTask(scheduler *tasks.Module) {
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID: "thumbnail-generation", Name: "Thumbnail Generation",
		Description: "Generates missing thumbnails for media files",
		Schedule:    30 * time.Minute,
		Func: func(ctx context.Context) error {
			items := s.Media.ListMedia(media.Filter{})
			queued := 0
			for _, item := range items {
				if ctx.Err() != nil {
					break
				}
				isAudio := item.Type == "audio"
				needsGen := (isAudio && !s.Thumbnails.HasThumbnail(thumbnails.MediaID(item.ID))) ||
					(!isAudio && !s.Thumbnails.HasAllPreviewThumbnails(thumbnails.MediaID(item.ID)))
				if needsGen {
					_, err := s.Thumbnails.GenerateThumbnailRequest(&thumbnails.ThumbnailRequest{
						MediaPath: item.Path, MediaID: item.ID,
						IsAudio: isAudio, HighPriority: false,
					})
					if err == nil || errors.Is(err, thumbnails.ErrThumbnailPending) {
						queued++
					}
				}
			}
			if queued > 0 {
				s.log.Info("Queued %d thumbnail generation jobs", queued)
			}
			return nil
		},
	})
}

func (s *Server) registerBackupTask(scheduler *tasks.Module) {
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID: "backup-cleanup", Name: "Backup Cleanup",
		Description: "Removes old backups beyond the configured retention count",
		Schedule:    24 * time.Hour,
		Func: func(ctx context.Context) error {
			keepCount := s.cfg.Get().Backup.RetentionCount
			if keepCount <= 0 {
				keepCount = 10
			}
			removed, err := s.Backup.CleanOldBackups(keepCount)
			if removed > 0 {
				s.log.Info("Cleaned %d old backups (keeping %d)", removed, keepCount)
			}
			return err
		},
	})

	// Scheduled automatic backup — runs at the configured interval but only
	// creates a backup when schedule_enabled is true in the backup config.
	scheduleInterval := s.cfg.Get().Backup.ScheduleInterval
	if scheduleInterval < 1*time.Hour {
		scheduleInterval = 24 * time.Hour
	}
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID: "scheduled-backup", Name: "Scheduled Backup",
		Description: "Creates an automatic full backup at the configured interval",
		Schedule:    scheduleInterval,
		Func: func(ctx context.Context) error {
			backupCfg := s.cfg.Get().Backup
			if !backupCfg.ScheduleEnabled {
				return nil
			}
			manifest, err := s.Backup.CreateBackup(backup.CreateBackupOptions{
				Description: "Scheduled automatic backup",
				Type:        "full",
			})
			if err != nil {
				return fmt.Errorf("scheduled backup failed: %w", err)
			}
			s.log.Info("Scheduled backup created: %s (%d files)", manifest.Filename, len(manifest.Files))
			return nil
		},
	})
}

func (s *Server) registerScannerTasks(scheduler *tasks.Module) {
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID: "mature-content-scan", Name: "Mature Content Scan",
		Description: "Scans media directories for mature content using configured detection models",
		Schedule:    12 * time.Hour,
		Func: func(ctx context.Context) error {
			dirs := s.cfg.Get().Directories
			var allResults []*scanner.ScanResult
			for _, dir := range []string{dirs.Videos, dirs.Music, dirs.Uploads} {
				if dir == "" {
					continue
				}
				results, err := s.Scanner.ScanDirectory(dir)
				if err != nil {
					s.log.Error("Mature scan failed for %s: %v", dir, err)
					continue
				}
				allResults = append(allResults, results...)
			}
			applied := 0
			for _, result := range allResults {
				if result.AutoFlagged && result.IsMature {
					if err := s.Media.SetMatureFlag(result.Path, true, result.Confidence, result.Reasons); err != nil {
						s.log.Error("Failed to set mature flag for %s: %v", result.Path, err)
					} else {
						applied++
					}
				}
			}
			if applied > 0 {
				s.log.Info("Mature scan complete: %d scanned, %d flagged", len(allResults), applied)
			}
			return nil
		},
	})

	if s.Scanner.HasHuggingFace() {
		scheduler.RegisterTask(tasks.TaskRegistration{
			ID: "hf-classification", Name: "Hugging Face Classification",
			Description: "Runs visual classification on mature content that has not been tagged yet",
			Schedule:    12 * time.Hour,
			Func: func(ctx context.Context) error {
				items := s.Media.ListMedia(media.Filter{})
				classified := 0
				for _, item := range items {
					if ctx.Err() != nil {
						break
					}
					if !item.IsMature || len(item.Tags) > 0 {
						continue
					}
					tags, err := s.Scanner.ClassifyMatureContent(ctx, item.Path)
					if err != nil {
						s.log.Warn("HF classification failed for %s: %v", item.Path, err)
						continue
					}
					if len(tags) > 0 {
						if err := s.Media.UpdateTags(item.Path, tags); err != nil {
							s.log.Warn("Failed to update tags for %s: %v", item.Path, err)
						} else {
							classified++
						}
					}
				}
				if classified > 0 {
					s.log.Info("HF classification: tagged %d mature items", classified)
				}
				return nil
			},
		})
	}
}

func (s *Server) registerHealthCheck(scheduler *tasks.Module) {
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID: "health-check", Name: "Health Check",
		Description: "Performs system diagnostics and monitors disk space",
		Schedule:    5 * time.Minute,
		Func: func(ctx context.Context) error {
			appCfg := s.cfg.Get()
			dirs := appCfg.Directories
			for name, dir := range map[string]string{"videos": dirs.Videos, "data": dirs.Data, "logs": dirs.Logs} {
				if dir == "" {
					continue
				}
				if _, err := os.Stat(dir); err != nil {
					s.log.Warn("Health check: %s path %q not accessible: %v", name, dir, err)
				} else {
					s.log.Debug("Health check: %s dir %q exists", name, dir)
				}
			}
			s.log.Debug("Periodic health check complete")
			return nil
		},
	})
}

func (s *Server) registerHLSTask(scheduler *tasks.Module) {
	pregenInterval := time.Duration(s.cfg.Get().HLS.PreGenerateIntervalHours) * time.Hour
	if pregenInterval < 15*time.Minute {
		pregenInterval = 15 * time.Minute
	}
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID: "hls-pregenerate", Name: "HLS Pre-generation",
		Description: "Pre-generates HLS streaming content for video files that don't have it yet",
		Schedule:    pregenInterval,
		Func: func(ctx context.Context) error {
			if !s.HLS.IsAvailable() || !s.cfg.Get().HLS.AutoGenerate {
				return nil
			}
			if active := s.HLS.ActiveJobCount(); active > 0 {
				s.log.Debug("HLS pre-generation: %d jobs already active, skipping this cycle", active)
				return nil
			}
			batchLimit := s.cfg.Get().HLS.ConcurrentLimit
			if batchLimit <= 0 {
				batchLimit = 2
			}
			items := s.Media.ListMedia(media.Filter{})
			queued := 0
			for _, item := range items {
				if ctx.Err() != nil || queued >= batchLimit {
					break
				}
				if item.Type != "video" || s.HLS.HasHLS(item.Path) {
					continue
				}
				if _, err := s.HLS.GenerateHLS(ctx, &hls.GenerateHLSParams{
					MediaPath: item.Path, MediaID: item.ID,
				}); err != nil {
					s.log.Debug("HLS pre-generation skipped for %s: %v", item.Name, err)
					continue
				}
				queued++
			}
			if queued > 0 {
				s.log.Info("Queued %d HLS pre-generation jobs (batch limit: %d)", queued, batchLimit)
			}
			return nil
		},
	})
}
