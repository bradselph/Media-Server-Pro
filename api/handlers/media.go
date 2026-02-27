package handlers

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/media"
	"media-server-pro/internal/streaming"
	"media-server-pro/internal/thumbnails"
	"media-server-pro/pkg/models"
)

// ListMedia returns all media items
func (h *Handler) ListMedia(c *gin.Context) {
	c.Header("Cache-Control", "private, max-age=60")

	sortBy := c.Query("sort")
	if sortBy == "date" {
		sortBy = "date_modified"
	}

	var limit, offset int
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 {
		if l > 500 {
			l = 500
		}
		limit = l
	}
	if o, err := strconv.Atoi(c.Query("offset")); err == nil && o > 0 {
		if o > 50000 {
			o = 50000
		}
		offset = o
	}

	var tags []string
	if t := c.Query("tags"); t != "" {
		tags = strings.Split(t, ",")
	}

	var isMature *bool
	if im := c.Query("is_mature"); im != "" {
		v := im == "true" || im == "1"
		isMature = &v
	}

	filterNoPagination := media.Filter{
		Type:     models.MediaType(c.Query("type")),
		Category: c.Query("category"),
		Search:   c.Query("search"),
		Tags:     tags,
		IsMature: isMature,
		SortBy:   sortBy,
		SortDesc: c.Query("sort_order") == "desc",
	}

	allItems := h.media.ListMedia(filterNoPagination)

	session := getSession(c)
	hideMature := false
	if session != nil {
		user, err := h.auth.GetUser(c.Request.Context(), session.Username)
		if err != nil || user == nil || !user.Preferences.ShowMature {
			hideMature = true
		}
	}

	if hideMature {
		filtered := make([]*models.MediaItem, 0, len(allItems))
		for _, item := range allItems {
			if !item.IsMature {
				filtered = append(filtered, item)
			}
		}
		allItems = filtered
	}

	totalItems := len(allItems)
	totalPages := 1
	if limit > 0 {
		totalPages = (totalItems + limit - 1) / limit
		if totalPages < 1 {
			totalPages = 1
		}
	}

	items := allItems
	if items == nil {
		items = []*models.MediaItem{}
	}
	if offset > 0 {
		if offset >= len(items) {
			items = []*models.MediaItem{}
		} else {
			items = items[offset:]
		}
	}
	if limit > 0 && limit < len(items) {
		items = items[:limit]
	}

	for _, item := range items {
		if item.ThumbnailURL == "" {
			if !h.thumbnails.HasThumbnail(item.Path) {
				isAudio := item.Type == "audio"
				_, err := h.thumbnails.GenerateThumbnail(item.Path, isAudio)
				if err != nil && !errors.Is(err, thumbnails.ErrThumbnailPending) {
					h.log.Warn("Failed to queue thumbnail for %s: %v", item.Path, err)
				}
			}
			item.ThumbnailURL = h.thumbnails.GetThumbnailURL(item.Path)
		}
	}

	writeSuccess(c, map[string]interface{}{
		"items":       items,
		"total_items": totalItems,
		"total_pages": totalPages,
		"scanning":    h.media.IsScanning(),
	})
}

// GetMedia returns a single media item
func (h *Handler) GetMedia(c *gin.Context) {
	id := c.Param("id")

	if decoded, err := url.PathUnescape(id); err == nil {
		id = decoded
	}

	item, err := h.media.GetMedia(id)
	if err != nil {
		item, err = h.media.GetMediaByID(id)
		if err != nil {
			writeError(c, http.StatusNotFound, "Media not found")
			return
		}
	}

	if item.ThumbnailURL == "" {
		if !h.thumbnails.HasThumbnail(item.Path) {
			isAudio := item.Type == "audio"
			_, err := h.thumbnails.GenerateThumbnail(item.Path, isAudio)
			if err != nil && !errors.Is(err, thumbnails.ErrThumbnailPending) {
				h.log.Warn("Failed to queue thumbnail for %s: %v", item.Path, err)
			}
		}
		item.ThumbnailURL = h.thumbnails.GetThumbnailURL(item.Path)
	}

	writeSuccess(c, item)
}

// GetMediaStats returns media statistics
func (h *Handler) GetMediaStats(c *gin.Context) {
	stats := h.media.GetStats()
	writeSuccess(c, stats)
}

// ScanMedia initiates a media scan
func (h *Handler) ScanMedia(c *gin.Context) {
	go func() {
		if err := h.media.Scan(); err != nil {
			h.log.Error("Media scan failed: %v", err)
		}
	}()
	writeSuccess(c, map[string]string{"message": "Scan started"})
}

// GetCategories returns media categories
func (h *Handler) GetCategories(c *gin.Context) {
	categories := h.media.GetCategories()
	writeSuccess(c, categories)
}

// StreamMedia streams a media file
func (h *Handler) StreamMedia(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		writeError(c, http.StatusBadRequest, errPathRequired)
		return
	}

	absPath, ok := h.resolveAndValidatePath(c, path, h.allowedMediaDirs())
	if !ok {
		return
	}

	if !h.checkMatureAccess(c, absPath) {
		return
	}

	session := getSession(c)
	var userID, sessionID string
	if session != nil {
		userID = session.UserID
		sessionID = session.ID

		user, err := h.auth.GetUser(c.Request.Context(), session.Username)
		if err == nil {
			maxStreams := h.getUserStreamLimit(user.Type)
			if maxStreams > 0 && !h.streaming.CanStartStream(userID, maxStreams) {
				writeError(c, http.StatusTooManyRequests, "Maximum concurrent streams limit reached")
				return
			}
		}
	}

	req := streaming.StreamRequest{
		Path:        absPath,
		Quality:     c.Query("quality"),
		UserID:      userID,
		SessionID:   sessionID,
		IPAddress:   c.ClientIP(),
		UserAgent:   c.Request.UserAgent(),
		RangeHeader: c.Request.Header.Get("Range"),
	}

	rangeHeader := c.Request.Header.Get("Range")
	isInitialRequest := rangeHeader == "" || strings.HasPrefix(rangeHeader, "bytes=0-")
	if isInitialRequest && session == nil && h.analytics != nil {
		h.analytics.TrackView(c.Request.Context(), absPath, userID, sessionID, req.IPAddress, req.UserAgent)
	}

	if h.suggestions != nil && userID != "" {
		if item, err := h.media.GetMedia(absPath); err == nil {
			h.suggestions.RecordView(userID, absPath, item.Category, string(item.Type), 0)
		}
	}

	if err := h.media.IncrementViews(c.Request.Context(), absPath); err != nil {
		h.log.Warn("Failed to increment view count for %s: %v", absPath, err)
	}

	if err := h.streaming.Stream(c.Writer, c.Request, req); err != nil {
		if errors.Is(err, streaming.ErrFileNotFound) {
			writeError(c, http.StatusNotFound, errFileNotFound)
		} else {
			h.log.Error("Stream error: %v", err)
			writeError(c, http.StatusInternalServerError, "Stream error")
		}
	}
}

// DownloadMedia downloads a media file
func (h *Handler) DownloadMedia(c *gin.Context) {
	cfg := h.media.GetConfig()
	session := getSession(c)

	if cfg.Download.RequireAuth && session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	if session != nil {
		user, err := h.auth.GetUser(c.Request.Context(), session.Username)
		if err == nil && !user.Permissions.CanDownload {
			writeError(c, http.StatusForbidden, "Download not allowed for your user type")
			return
		}
	}

	path := c.Query("path")
	if path == "" {
		writeError(c, http.StatusBadRequest, errPathRequired)
		return
	}

	absPath, ok := h.resolveAndValidatePath(c, path, h.allowedMediaDirs())
	if !ok {
		return
	}

	if !h.checkMatureAccess(c, absPath) {
		return
	}

	if err := h.streaming.Download(c.Writer, c.Request, absPath); err != nil {
		if errors.Is(err, streaming.ErrFileNotFound) {
			writeError(c, http.StatusNotFound, errFileNotFound)
		} else if errors.Is(err, streaming.ErrFileTooLarge) {
			writeError(c, http.StatusRequestEntityTooLarge, "File exceeds maximum download size")
		} else if isClientDisconnect(err) {
			h.log.Debug("Download cancelled by client: %v", err)
		} else {
			h.log.Error("Download error: %v", err)
			writeError(c, http.StatusInternalServerError, "Download error")
		}
	}
}

// GetPlaybackPosition returns the saved playback position for the current user.
func (h *Handler) GetPlaybackPosition(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		writeError(c, http.StatusBadRequest, errPathParamRequired)
		return
	}

	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}
	position := h.media.GetPlaybackPosition(c.Request.Context(), path, session.UserID)
	writeSuccess(c, map[string]float64{"position": position})
}

// TrackPlayback records playback position
func (h *Handler) TrackPlayback(c *gin.Context) {
	var req struct {
		Path     string  `json:"path"`
		Position float64 `json:"position"`
		Duration float64 `json:"duration"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	session := getSession(c)
	var userID, sessionID, username string
	if session != nil {
		userID = session.UserID
		sessionID = session.ID
		username = session.Username
	}

	if userID != "" {
		if err := h.media.UpdatePlaybackPosition(c.Request.Context(), req.Path, userID, req.Position); err != nil {
			h.log.Warn("Failed to update playback position for %s: %v", req.Path, err)
		}

		if req.Duration > 0 && username != "" {
			pathHash := md5.Sum([]byte(req.Path))
			item := models.WatchHistoryItem{
				MediaPath: req.Path,
				MediaID:   hex.EncodeToString(pathHash[:]),
				Position:  req.Position,
				Duration:  req.Duration,
				WatchedAt: time.Now(),
			}
			item.Progress = req.Position / req.Duration
			item.Completed = item.Progress >= 0.9
			if err := h.auth.AddToWatchHistory(c.Request.Context(), username, item); err != nil {
				h.log.Debug("Watch history update skipped for %s: %v", req.Path, err)
			}
		}
	}

	if h.analytics != nil {
		h.analytics.TrackPlayback(c.Request.Context(), req.Path, userID, sessionID, req.Position, req.Duration)
	}

	writeSuccess(c, nil)
}
