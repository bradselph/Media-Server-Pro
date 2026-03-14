package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"fmt"
	"media-server-pro/internal/analytics"
	"media-server-pro/internal/media"
	"media-server-pro/internal/streaming"
	"media-server-pro/internal/thumbnails"
	"media-server-pro/pkg/models"
)

// ListMedia returns all media items
func (h *Handler) ListMedia(c *gin.Context) {
	c.Header("Cache-Control", "private, max-age=300")

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

	// Merge receiver (slave) media into the listing so users see one unified library.
	// Receiver items are indistinguishable from local media to regular users.
	// Duplicate detection: skip receiver items whose content fingerprint matches a
	// local item (same file exists on both master and slave — show only the local copy).
	hasReceiverItems := false
	if h.receiver != nil {
		if h.media.GetConfig().Receiver.Enabled {
			// Track fingerprints already added from receiver items so that if
			// the same file exists on two different slaves, only the first is kept.
			seenFP := make(map[string]bool)
			for _, ri := range h.receiver.GetAllMedia() {
				if ri.ContentFingerprint != "" {
					// Skip master-vs-slave duplicates
					if h.media.HasFingerprint(ri.ContentFingerprint) {
						continue
					}
					// Skip slave-vs-slave duplicates
					if seenFP[ri.ContentFingerprint] {
						continue
					}
					seenFP[ri.ContentFingerprint] = true
				}
				item := &models.MediaItem{
					ID:       ri.ID,
					Name:     ri.Name,
					Type:     models.MediaType(ri.MediaType),
					Size:     ri.Size,
					Duration: ri.Duration,
					Width:    ri.Width,
					Height:   ri.Height,
					IsMature: h.isReceiverItemMature(ri.ContentFingerprint),
				}
				// Apply the exact same filter logic as local media (category,
				// tags, search, type, is_mature — not just type+search).
				if !filterNoPagination.Matches(item) {
					continue
				}
				allItems = append(allItems, item)
				hasReceiverItems = true
			}
		}
	}

	// Merge extractor items into the listing so extracted external URLs
	// appear in the unified library alongside local and slave media.
	if h.extractor != nil {
		if h.media.GetConfig().Extractor.Enabled {
			for _, ei := range h.extractor.GetAllItems() {
				if ei.Status != "active" {
					continue
				}
				item := &models.MediaItem{
					ID:   ei.ID,
					Name: ei.Title,
					Type: models.MediaTypeVideo,
				}
				if !filterNoPagination.Matches(item) {
					continue
				}
				allItems = append(allItems, item)
				hasReceiverItems = true // reuse flag to trigger re-sort
			}
		}
	}

	// Re-sort the combined list so receiver/extractor items are interleaved correctly
	// with local items instead of being appended at the end.
	if hasReceiverItems {
		filterNoPagination.SortItems(allItems)
	}

	// Mature content: always include mature items in the listing so the
	// frontend can render them blurred with a gate overlay (sign-in prompt
	// for guests, enable-in-settings prompt for authenticated users).
	// Actual playback/streaming is blocked by checkMatureAccess().

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
		if item.ThumbnailURL == "" && item.Path != "" && h.thumbnails != nil {
			// Only generate thumbnails for local media (receiver items have no local path)
			if !h.thumbnails.HasThumbnail(thumbnails.MediaID(item.ID)) {
				isAudio := item.Type == "audio"
				_, err := h.thumbnails.GenerateThumbnailRequest(&thumbnails.ThumbnailRequest{MediaPath: item.Path, MediaID: item.ID, IsAudio: isAudio, HighPriority: true})
				if err != nil && !errors.Is(err, thumbnails.ErrThumbnailPending) {
					h.log.Warn("Failed to queue thumbnail for %s: %v", item.Path, err)
				}
			}
			item.ThumbnailURL = h.thumbnails.GetThumbnailURL(thumbnails.MediaID(item.ID))
		}
	}

	resp := map[string]interface{}{
		"items":       items,
		"total_items": totalItems,
		"total_pages": totalPages,
		"scanning":    h.media.IsScanning(),
	}
	if !h.media.IsReady() {
		resp["initializing"] = true
	}
	writeSuccess(c, resp)
}

// GetMedia returns a single media item
func (h *Handler) GetMedia(c *gin.Context) {
	id := c.Param("id")

	item, err := h.media.GetMediaByID(id)
	if err != nil {
		// Try receiver media — return it as a models.MediaItem so it's transparent
		if h.receiver != nil {
			if ri := h.receiver.GetMediaItem(id); ri != nil {
				// Check mature access for receiver items via fingerprint
				if h.isReceiverItemMature(ri.ContentFingerprint) && !h.canViewMatureContent(c) {
					writeError(c, http.StatusForbidden,
						"This content is marked as mature (18+). Please log in and enable mature content to access it.")
					return
				}
				writeSuccess(c, &models.MediaItem{
					ID:       ri.ID,
					Name:     ri.Name,
					Type:     models.MediaType(ri.MediaType),
					Size:     ri.Size,
					Duration: ri.Duration,
					Width:    ri.Width,
					Height:   ri.Height,
					IsMature: h.isReceiverItemMature(ri.ContentFingerprint),
				})
				return
			}
		}
		// Try extractor items
		if h.extractor != nil {
			if ei := h.extractor.GetItem(id); ei != nil && ei.Status == "active" {
				writeSuccess(c, &models.MediaItem{
					ID:   ei.ID,
					Name: ei.Title,
					Type: models.MediaTypeVideo,
				})
				return
			}
		}
		if !h.media.IsReady() {
			c.Header("Retry-After", "3")
			writeError(c, http.StatusServiceUnavailable, "Server is initializing — media library scan in progress, please try again shortly")
			return
		}
		writeError(c, http.StatusNotFound, errMediaNotFound)
		return
	}

	// Check mature content access before returning individual item (same 3-layer
	// check as checkMatureAccess: session → permission → preference).
	if item.IsMature {
		session := getSession(c)
		if session == nil {
			writeError(c, http.StatusForbidden, "This content is marked as mature (18+). Please log in to access it.")
			return
		}
		user := getUser(c)
		if user == nil {
			writeError(c, http.StatusForbidden, "This content is marked as mature (18+). Enable mature content in your preferences to access it.")
			return
		}
		if !user.Permissions.CanViewMature {
			writeError(c, http.StatusForbidden, "Your account does not have permission to view mature content (18+).")
			return
		}
		if !user.Preferences.ShowMature {
			writeError(c, http.StatusForbidden, "This content is marked as mature (18+). Enable mature content in your preferences to access it.")
			return
		}
	}

	if item.ThumbnailURL == "" && h.thumbnails != nil {
		if !h.thumbnails.HasThumbnail(thumbnails.MediaID(item.ID)) {
			isAudio := item.Type == "audio"
			_, err := h.thumbnails.GenerateThumbnailRequest(&thumbnails.ThumbnailRequest{MediaPath: item.Path, MediaID: item.ID, IsAudio: isAudio, HighPriority: true})
			if err != nil && !errors.Is(err, thumbnails.ErrThumbnailPending) {
				h.log.Warn("Failed to queue thumbnail for %s: %v", item.Path, err)
			}
		}
		item.ThumbnailURL = h.thumbnails.GetThumbnailURL(thumbnails.MediaID(item.ID))
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
	id := c.Query("id")
	if id == "" {
		writeError(c, http.StatusBadRequest, errIDRequired)
		return
	}

	// Try local media first
	localItem, localErr := h.media.GetMediaByID(id)
	if localErr != nil {
		// Not found locally — try receiver media (slave-sourced).
		// This makes slave media fully transparent — same URL pattern as local.
		if h.receiver != nil {
			if item := h.receiver.GetMediaItem(id); item != nil {
				// Receiver items inherit the mature flag from the scanner if the
				// master has scanned the same content. If the fingerprint matches a
				// mature local item, deny access for unauthorised callers.
				if h.isReceiverItemMature(item.ContentFingerprint) && !h.canViewMatureContent(c) {
					writeError(c, http.StatusForbidden,
						"This content is marked as mature (18+). Please log in and enable mature content to access it.")
					return
				}
				// Enforce per-user stream limits for receiver-sourced media, same as local media.
				if session := getSession(c); session != nil {
					if user, err := h.auth.GetUser(c.Request.Context(), session.Username); err == nil {
						maxStreams := h.getUserStreamLimit(user.Type)
						if maxStreams > 0 && !h.streaming.CanStartStream(session.UserID, maxStreams) {
							writeError(c, http.StatusTooManyRequests, "Maximum concurrent streams limit reached")
							return
						}
					}
				}
				if err := h.receiver.ProxyStream(c.Writer, c.Request, id); err != nil {
					if !c.Writer.Written() && !isClientDisconnect(err) {
						writeError(c, http.StatusBadGateway, "Stream proxy error")
					}
				}
				return
			}
		}
		// Try extractor items — proxy HLS from M3U8 stream
		if h.extractor != nil {
			if ei := h.extractor.GetItem(id); ei != nil && ei.Status == "active" {
				// Redirect to the proxy HLS master playlist
				c.Redirect(http.StatusFound, fmt.Sprintf("/extractor/hls/%s/master.m3u8", id))
				return
			}
		}
		// Neither local, receiver, nor extractor — write appropriate error
		if !h.media.IsReady() {
			c.Header("Retry-After", "3")
			writeError(c, http.StatusServiceUnavailable, "Server is initializing — media library scan in progress, please try again shortly")
		} else {
			writeError(c, http.StatusNotFound, errMediaNotFound)
		}
		return
	}
	absPath := localItem.Path

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

	isInitialRequest := req.RangeHeader == "" || strings.HasPrefix(req.RangeHeader, "bytes=0-")
	if isInitialRequest && h.analytics != nil {
		// Use the stable UUID (id) so analytics keys match client-submitted events.
		h.analytics.TrackView(c.Request.Context(), analytics.ViewParams{
			MediaID:   id,
			UserID:    userID,
			SessionID: sessionID,
			IPAddress: req.IPAddress,
			UserAgent: req.UserAgent,
		})
	}

	if h.suggestions != nil && userID != "" {
		if item, err := h.media.GetMedia(absPath); err == nil && item != nil {
			h.suggestions.RecordView(userID, absPath, item.Category, string(item.Type), 0)
		}
	}

	if err := h.media.IncrementViews(c.Request.Context(), absPath); err != nil {
		h.log.Warn("Failed to increment view count for %s: %v", absPath, err)
	}

	// When streaming fails due to client disconnect (broken pipe, connection reset),
	// we check c.Writer.Written() and isClientDisconnect(err) before writing an error
	// to avoid corrupting a partially written response.
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

	// TODO: When RequireAuth is false and the user is unauthenticated (session == nil),
	// the CanDownload permission check is skipped entirely, allowing anonymous downloads
	// with no permission enforcement. If downloads should always require auth, this block
	// should use the user from context (getUser) rather than re-fetching; if anonymous
	// downloads are intentional, this is fine but should be documented.
	if session != nil {
		user, err := h.auth.GetUser(c.Request.Context(), session.Username)
		if err != nil || user == nil {
			writeError(c, http.StatusInternalServerError, "Failed to retrieve user permissions")
			return
		}
		if !user.Permissions.CanDownload {
			writeError(c, http.StatusForbidden, "Download not allowed for your user type")
			return
		}
	}

	id := c.Query("id")
	if id == "" {
		writeError(c, http.StatusBadRequest, errIDRequired)
		return
	}

	localItem, localErr := h.media.GetMediaByID(id)
	if localErr != nil {
		// Not found locally — try receiver media for download too
		if h.receiver != nil {
			if item := h.receiver.GetMediaItem(id); item != nil {
				if h.isReceiverItemMature(item.ContentFingerprint) && !h.canViewMatureContent(c) {
					writeError(c, http.StatusForbidden,
						"This content is marked as mature (18+). Please log in and enable mature content to access it.")
					return
				}
				if err := h.receiver.ProxyStream(c.Writer, c.Request, id); err != nil {
					if !c.Writer.Written() && !isClientDisconnect(err) {
						writeError(c, http.StatusBadGateway, "Download proxy error")
					}
				}
				return
			}
		}
		writeError(c, http.StatusNotFound, errMediaNotFound)
		return
	}
	absPath := localItem.Path

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
	id := c.Query("id")
	mediaPath, _, ok := h.resolveMediaPathOrReceiver(c, id)
	if !ok {
		return
	}

	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}
	position := h.media.GetPlaybackPosition(c.Request.Context(), mediaPath, session.UserID)
	writeSuccess(c, map[string]float64{"position": position})
}

// TrackPlayback records playback position
func (h *Handler) TrackPlayback(c *gin.Context) {
	var req struct {
		ID       string  `json:"id"`
		Position float64 `json:"position"`
		Duration float64 `json:"duration"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if req.Position < 0 {
		req.Position = 0
	}

	mediaPath, mediaName, ok := h.resolveMediaPathOrReceiver(c, req.ID)
	if !ok {
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
		if err := h.media.UpdatePlaybackPosition(c.Request.Context(), mediaPath, userID, req.Position); err != nil {
			h.log.Warn("Failed to update playback position for media %s: %v", req.ID, err)
		}

		if req.Duration > 0 && username != "" {
			// For local media, prefer the name from the media module; for
			// receiver items the name was already resolved by the helper.
			if mi, err := h.media.GetMedia(mediaPath); err == nil {
				mediaName = mi.Name
			}
			item := models.WatchHistoryItem{
				MediaPath: mediaPath,
				MediaID:   req.ID,
				MediaName: mediaName,
				Position:  req.Position,
				Duration:  req.Duration,
				WatchedAt: time.Now(),
			}
			item.Progress = req.Position / req.Duration
			item.Completed = item.Progress >= 0.9
			if err := h.auth.AddToWatchHistory(c.Request.Context(), username, item); err != nil {
				h.log.Debug("Watch history update skipped for media %s: %v", req.ID, err)
			}

			if item.Completed && h.suggestions != nil {
				h.suggestions.RecordCompletion(userID, mediaPath)
			}
		}
	}

	if h.analytics != nil {
		// Use the stable UUID so analytics keys match client-submitted events.
		h.analytics.TrackPlayback(c.Request.Context(), analytics.PlaybackParams{
			MediaID:   req.ID,
			UserID:    userID,
			SessionID: sessionID,
			Position:  req.Position,
			Duration:  req.Duration,
		})
	}

	writeSuccess(c, nil)
}

// canViewMatureContent reports whether the current request's user is authorised
// to access mature content (session + CanViewMature permission + ShowMature pref).
func (h *Handler) canViewMatureContent(c *gin.Context) bool {
	user := getUser(c)
	if user == nil {
		return false
	}
	return user.Permissions.CanViewMature && user.Preferences.ShowMature
}

// isReceiverItemMature checks whether a receiver item's content fingerprint
// matches a local item that is flagged as mature. Returns false if the
// fingerprint is empty or unknown (errs on the side of allowing access when
// mature status is indeterminate).
func (h *Handler) isReceiverItemMature(fingerprint string) bool {
	if fingerprint == "" {
		return false
	}
	return h.media.IsFingerprintMature(fingerprint)
}
