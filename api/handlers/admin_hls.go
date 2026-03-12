package handlers

import (
	"github.com/gin-gonic/gin"
)

// GetHLSStats is implemented in hls.go (h.GetHLSStats).
// ListHLSJobs is implemented in hls.go (h.ListHLSJobs).
// DeleteHLSJob is implemented in hls.go (h.DeleteHLSJob).
// ValidateHLS is implemented in hls.go (h.ValidateHLS).
// CleanHLSStaleLocks is implemented in hls.go (h.CleanHLSStaleLocks).
// CleanHLSInactive is implemented in hls.go (h.CleanHLSInactive).

// adminHLSPlaceholder ensures the file compiles even though all admin HLS handlers
// live in hls.go (no duplication needed).
// TODO(feature-gap): This file is a placeholder only; no handler logic lives here. Routes
// GET/POST/DELETE /api/admin/hls/* are registered in routes.go and implemented in hls.go.
// Remove this file or keep a single package doc — the var _ trick is redundant.
var _ = (*gin.Context)(nil)
