package handlers

import (
	"reflect"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

// AdminGetStats returns admin statistics.
func (h *Handler) AdminGetStats(c *gin.Context) {
	if !h.requireAdminModule(c) {
		return
	}
	adminStats := h.admin.GetServerStats()
	mediaStats := h.media.GetStats()
	streamStats := h.streaming.GetStats()
	var hlsRunning, hlsCompleted int
	if h.hls != nil {
		hlsStats := h.hls.GetStats()
		hlsRunning = hlsStats.RunningJobs
		hlsCompleted = hlsStats.CompletedJobs
	}

	totalUsers := len(h.auth.ListUsers(c.Request.Context()))

	var totalViews int
	if h.analytics != nil {
		totalViews = h.analytics.GetStats().TotalViews
	}

	var diskTotal, diskFree uint64
	cfg := h.media.GetConfig()
	if cfg.Directories.Videos != "" {
		if du, err := helpers.GetDiskUsage(cfg.Directories.Videos); err == nil {
			diskTotal = du.Total
			diskFree = du.Available
		} else {
			h.log.Warn("GetDiskUsage failed for %s: %v", cfg.Directories.Videos, err)
		}
	}
	var diskUsed uint64
	if diskTotal > diskFree {
		diskUsed = diskTotal - diskFree
	}

	writeSuccess(c, map[string]any{
		"total_videos":       mediaStats.VideoCount,
		"total_audio":        mediaStats.AudioCount,
		"active_sessions":    streamStats.ActiveStreams,
		"total_users":        totalUsers,
		"disk_usage":         diskUsed,
		"disk_total":         diskTotal,
		"disk_free":          diskFree,
		"hls_jobs_running":   hlsRunning,
		"hls_jobs_completed": hlsCompleted,
		"server_uptime":      int64(adminStats.Uptime.Seconds()),
		"total_views":        totalViews,
	})
}

// AdminGetSystemInfo returns system information shaped for the frontend SystemInfo type.
func (h *Handler) AdminGetSystemInfo(c *gin.Context) {
	if !h.requireAdminModule(c) {
		return
	}
	info := h.admin.GetSystemInfo()
	uptimeSecs := h.admin.GetUptimeSecs()

	type healthier interface {
		Health() models.HealthStatus
	}
	// Build module list, skipping any that are nil (optional modules that failed to init).
	// NOTE: A typed nil pointer (e.g. (*analytics.Module)(nil)) stored in an interface
	// is NOT nil at the interface level — we must use reflect to detect it.
	allModules := []healthier{
		h.security, h.database, h.auth, h.media, h.streaming, h.hls,
		h.analytics, h.playlist, h.admin, h.tasks, h.upload, h.scanner,
		h.thumbnails, h.validator, h.backup, h.autodiscovery, h.suggestions,
		h.categorizer, h.updater, h.remote, h.receiver,
		h.extractor, h.crawler, h.duplicates, h.downloader,
		h.claude,
	}
	// last_check is always present (CheckedAt is always set); no omitempty so the contract is explicit.
	type moduleHealthItem struct {
		Name      string `json:"name"`
		Status    string `json:"status"`
		Message   string `json:"message,omitempty"`
		LastCheck string `json:"last_check"`
	}
	moduleHealths := make([]moduleHealthItem, 0, len(allModules))
	for _, p := range allModules {
		if p == nil || reflect.ValueOf(p).IsNil() {
			continue
		}
		hs := p.Health()
		moduleHealths = append(moduleHealths, moduleHealthItem{
			Name:      hs.Name,
			Status:    hs.Status,
			Message:   hs.Message,
			LastCheck: hs.CheckedAt.Format(time.RFC3339),
		})
	}

	writeSuccess(c, map[string]any{
		"version":      h.buildInfo.Version,
		"build_date":   h.buildInfo.BuildDate,
		"os":           runtime.GOOS,
		"arch":         runtime.GOARCH,
		"go_version":   info.GoVersion,
		"cpu_count":    info.NumCPU,
		"memory_used":  info.MemAlloc,
		"memory_total": info.MemTotal,
		"uptime":       uptimeSecs,
		"modules":      moduleHealths,
	})
}
