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
// TODO: This file exists only as a placeholder. The unused variable trick (var _ = ...)
// is a code smell — a blank file with just a package declaration would suffice, or
// this file should be removed entirely since the handlers.go index file already
// documents the split.
var _ = (*gin.Context)(nil)
