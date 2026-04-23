package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/analytics"
	"media-server-pro/internal/categorizer"
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
	sortByRating := sortBy == "my_rating"
	if sortByRating {
		sortBy = ""
	}

	var minRating float64
	if mr := c.Query("min_rating"); mr != "" {
		if v, err := strconv.ParseFloat(mr, 64); err == nil && v > 0 {
			minRating = v
		}
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
		isMature = new(im == "true" || im == "1")
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

	// Track search queries for analytics (non-empty search terms only)
	if h.analytics != nil && filterNoPagination.Search != "" {
		sess := getSession(c)
		uid := ""
		if sess != nil {
			uid = sess.UserID
		}
		h.analytics.TrackTrafficEvent(c.Request.Context(), analytics.TrafficEventParams{
			Type: analytics.EventSearch, UserID: uid,
			IPAddress: c.ClientIP(), UserAgent: c.Request.UserAgent(),
			Data: map[string]any{"query": filterNoPagination.Search},
		})
	}

	allItems := h.media.ListMedia(filterNoPagination)

	// Global ID set — tracks every item ID already present in allItems so that
	// receiver and extractor items are never added twice regardless of source.
	seenIDs := make(map[string]bool, len(allItems))
	for _, item := range allItems {
		seenIDs[item.ID] = true
	}

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
				// Skip ID duplicates (same item from multiple sources)
				if seenIDs[ri.ID] {
					continue
				}
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
				seenIDs[ri.ID] = true
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
				// Skip ID duplicates (same item already present from local or receiver)
				if seenIDs[ei.ID] {
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
				seenIDs[ei.ID] = true
				hasReceiverItems = true // reuse flag to trigger re-sort
			}
		}
	}

	// Re-sort the combined list so receiver/extractor items are interleaved correctly
	// with local items instead of being appended at the end.
	if hasReceiverItems {
		filterNoPagination.SortItems(allItems)
	}

	// Build user ratings map (path → rating) for authenticated users.
	// Used for sort=my_rating, min_rating filter, and the user_ratings response field.
	var userRatingsByPath map[string]float64
	if session := getSession(c); session != nil && h.suggestions != nil {
		if profile := h.suggestions.GetUserProfile(session.UserID); profile != nil {
			userRatingsByPath = make(map[string]float64, len(profile.ViewHistory))
			for _, vh := range profile.ViewHistory {
				if vh.Rating > 0 && vh.MediaPath != "" {
					userRatingsByPath[vh.MediaPath] = vh.Rating
				}
			}
		}
	}

	// Filter to only items the user has rated at or above min_rating.
	if minRating > 0 && userRatingsByPath != nil {
		filtered := make([]*models.MediaItem, 0, len(allItems))
		for _, item := range allItems {
			if userRatingsByPath[item.Path] >= minRating {
				filtered = append(filtered, item)
			}
		}
		allItems = filtered
	}

	// Sort by the user's personal rating (desc by default, asc if sort_order=asc).
	if sortByRating && userRatingsByPath != nil {
		sortDesc := c.Query("sort_order") != "asc"
		sort.SliceStable(allItems, func(i, j int) bool {
			ri := userRatingsByPath[allItems[i].Path]
			rj := userRatingsByPath[allItems[j].Path]
			if sortDesc {
				return ri > rj
			}
			return ri < rj
		})
	}

	// Hide completed items when hide_watched=true (authenticated users only).
	// An item is "watched" when the user's ViewHistory entry has CompletedAt set
	// (i.e. they watched past 90% of the runtime).
	if (c.Query("hide_watched") == "true" || c.Query("hide_watched") == "1") && h.suggestions != nil {
		if session := getSession(c); session != nil {
			if profile := h.suggestions.GetUserProfile(session.UserID); profile != nil {
				completedPaths := make(map[string]bool, len(profile.ViewHistory))
				for _, vh := range profile.ViewHistory {
					if vh.CompletedAt != nil && vh.MediaPath != "" {
						completedPaths[vh.MediaPath] = true
					}
				}
				if len(completedPaths) > 0 {
					kept := make([]*models.MediaItem, 0, len(allItems))
					for _, item := range allItems {
						if !completedPaths[item.Path] {
							kept = append(kept, item)
						}
					}
					allItems = kept
				}
			}
		}
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

	// Build user_ratings map keyed by media ID for the current page items.
	// Only included when the user is authenticated and has rated at least one item.
	var userRatingsByID map[string]float64
	if userRatingsByPath != nil {
		for _, item := range items {
			if item.Path != "" {
				if r, ok := userRatingsByPath[item.Path]; ok {
					if userRatingsByID == nil {
						userRatingsByID = make(map[string]float64)
					}
					userRatingsByID[item.ID] = r
				}
			}
		}
	}

	typeCounts := make(map[string]int, 3)
	for _, item := range allItems {
		if item.Type != "" {
			typeCounts[string(item.Type)]++
		}
	}

	resp := map[string]any{
		"items":       items,
		"total_items": totalItems,
		"total_pages": totalPages,
		"scanning":    h.media.IsScanning(),
		"type_counts": typeCounts,
	}
	if userRatingsByID != nil {
		resp["user_ratings"] = userRatingsByID
	}
	if !h.media.IsReady() {
		resp["initializing"] = true
	}
	writeSuccess(c, resp)
}

// GetMedia returns a single media item
func (h *Handler) GetMedia(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))

	item, err := h.media.GetMediaByID(id)
	if err != nil {
		// Try receiver media — return it as a models.MediaItem so it's transparent
		if h.receiver != nil {
			if ri := h.receiver.GetMediaItem(id); ri != nil {
				// Check mature access for receiver items via fingerprint
				if h.isReceiverItemMature(ri.ContentFingerprint) && !h.canViewMatureContent(c) {
					writeError(c, http.StatusForbidden, msgMatureContent)
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
			c.Header(headerRetryAfter, "3")
			writeError(c, http.StatusServiceUnavailable, msgInitializing)
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
			writeError(c, http.StatusUnauthorized, "This content is marked as mature (18+). Please log in to access it.")
			return
		}
		user := getUser(c)
		if user == nil {
			// Valid session but user record unavailable — likely a transient DB error
			// in the auth middleware, not a preference mismatch. Return 503 so the
			// client retries rather than telling the user to go change a preference.
			h.log.Error("GetMedia: valid session for mature item %s but getUser returned nil (auth DB failure?)", id)
			writeError(c, http.StatusServiceUnavailable, "Unable to verify your account — please try again")
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
	if h.media.IsScanning() {
		writeSuccess(c, map[string]string{"message": "Scan already in progress"})
		return
	}
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

// GetCategoryBrowse returns user-facing categorized items for a given category.
// When no category is specified, returns category counts (stats).
// Accepts ?category=TV+Shows&limit=N (default 200, max 500).
func (h *Handler) GetCategoryBrowse(c *gin.Context) {
	if !h.requireCategorizer(c) {
		return
	}
	category := c.Query("category")
	limit := 200
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 500 {
		limit = l
	}

	if category == "" {
		stats := h.categorizer.GetStats()
		writeSuccess(c, stats)
		return
	}

	items := h.categorizer.GetByCategory(categorizer.Category(category))

	// Enrich with thumbnail URLs and duration from the media module
	type browseItem struct {
		ID           string      `json:"id"`
		Name         string      `json:"name"`
		Category     string      `json:"category"`
		Confidence   float64     `json:"confidence"`
		Duration     float64     `json:"duration,omitempty"`
		DetectedInfo any `json:"detected_info,omitempty"`
		ThumbnailURL string      `json:"thumbnail_url,omitempty"`
	}
	results := make([]browseItem, 0, len(items))
	for _, item := range items {
		bi := browseItem{
			ID:           item.ID,
			Name:         item.Name,
			Category:     string(item.Category),
			Confidence:   item.Confidence,
			DetectedInfo: item.DetectedInfo,
		}
		if h.media != nil && item.ID != "" {
			if mi, err := h.media.GetMediaByID(item.ID); err == nil && mi != nil {
				bi.Duration = mi.Duration
			}
		}
		if h.thumbnails != nil && item.ID != "" {
			bi.ThumbnailURL = h.thumbnails.GetThumbnailURL(thumbnails.MediaID(item.ID))
		}
		results = append(results, bi)
	}

	if limit < len(results) {
		results = results[:limit]
	}

	writeSuccess(c, map[string]any{
		"category": category,
		"items":    results,
		"total":    len(results),
	})
}

// StreamMedia streams a media file
func (h *Handler) StreamMedia(c *gin.Context) {
	id := strings.TrimSpace(c.Query("id"))
	if id == "" {
		writeError(c, http.StatusBadRequest, errIDRequired)
		return
	}

	session := getSession(c)
	streamCfg := h.media.GetConfig().Streaming
	if session == nil && streamCfg.RequireAuth {
		writeError(c, http.StatusUnauthorized, "Authentication required to stream media")
		return
	}
	// NOTE: unauthenticated IP-based stream limiting is enforced later, at the
	// point where TrackProxyStream is called (receiver proxy branch below). A
	// pre-flight CanStartStream check here without a matching TrackProxyStream
	// is a TOCTOU: two concurrent requests both pass the untracked check, then
	// both pass the second untracked check, allowing 2× the limit. The
	// authoritative check+track pair in the receiver branch is sufficient.

	// Try local media first
	localItem, localErr := h.media.GetMediaByID(id)
	if localErr != nil {
		// Not found locally — try receiver media (slave-sourced).
		// This makes slave media fully transparent — same URL pattern as local.
		if h.receiver != nil {
			if item := h.receiver.GetMediaItem(id); item != nil {
				// Receiver items inherit the mature flag from the scanner if the
				// master has scanned the same content. If the fingerprint matches a
				// mature local item, deny access for unauthorized callers.
				if h.isReceiverItemMature(item.ContentFingerprint) && !h.canViewMatureContent(c) {
					writeError(c, http.StatusForbidden, msgMatureContent)
					return
				}
				// Enforce per-user or per-IP stream limits for receiver-sourced media.
				if session != nil {
					streamKey := session.UserID
					maxStreams := 3 // default fallback if user lookup fails
					if user, err := h.auth.GetUser(c.Request.Context(), session.Username); err == nil {
						maxStreams = h.getUserStreamLimit(user.Type)
					}
					if maxStreams > 0 && !h.streaming.CanStartStream(streamKey, maxStreams) {
						writeError(c, http.StatusTooManyRequests, msgMaxStreams)
						return
					}
					// Track the proxy stream so the counter is decremented when the stream ends.
					release := h.streaming.TrackProxyStream(streamKey)
					defer release()
				} else if limit := streamCfg.UnauthStreamLimit; limit > 0 {
					ipKey := "ip:" + c.ClientIP()
					if !h.streaming.CanStartStream(ipKey, limit) {
						writeError(c, http.StatusTooManyRequests, msgMaxStreamsConn)
						return
					}
					release := h.streaming.TrackProxyStream(ipKey)
					defer release()
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
				// Enforce per-user stream limits before redirecting.
				// Note: extractor streams are redirect-based so slots are not held open;
				// CanStartStream counts only sessions from other stream types (local/receiver),
				// which still provides partial enforcement of the global limit.
				// Extractor items have no IsMature flag so mature content filtering is not applicable here.
				if session != nil {
					user, err := h.auth.GetUser(c.Request.Context(), session.Username)
					if err != nil {
						h.log.Warn("Failed to look up user %s for extractor stream limit check: %v", session.Username, err)
						writeError(c, http.StatusServiceUnavailable, "Unable to verify stream permissions")
						return
					}
					maxStreams := h.getUserStreamLimit(user.Type)
					if maxStreams > 0 && !h.streaming.CanStartStream(session.UserID, maxStreams) {
						writeError(c, http.StatusTooManyRequests, msgMaxStreams)
						return
					}
				} else if limit := streamCfg.UnauthStreamLimit; limit > 0 {
					ipKey := "ip:" + c.ClientIP()
					if !h.streaming.CanStartStream(ipKey, limit) {
						writeError(c, http.StatusTooManyRequests, msgMaxStreamsConn)
						return
					}
				}
				// Redirect to the proxy HLS master playlist
				c.Redirect(http.StatusFound, fmt.Sprintf("/extractor/hls/%s/master.m3u8", id))
				return
			}
		}
		// Neither local, receiver, nor extractor — write appropriate error
		if !h.media.IsReady() {
			c.Header(headerRetryAfter, "3")
			writeError(c, http.StatusServiceUnavailable, msgInitializing)
		} else {
			writeError(c, http.StatusNotFound, errMediaNotFound)
		}
		return
	}
	absPath := localItem.Path

	if !h.checkMatureAccess(c, localItem.IsMature) {
		return
	}

	var userID, sessionID string
	if session != nil {
		userID = session.UserID
		sessionID = session.ID

		user, err := h.auth.GetUser(c.Request.Context(), session.Username)
		if err != nil {
			h.log.Warn("Failed to look up user %s for stream limit check: %v", session.Username, err)
			// Fail closed: deny stream when user lookup fails rather than
			// allowing unlimited streams during transient DB outages.
			writeError(c, http.StatusServiceUnavailable, "Unable to verify stream permissions")
			return
		}
		maxStreams := h.getUserStreamLimit(user.Type)
		if maxStreams > 0 && !h.streaming.CanStartStream(userID, maxStreams) {
			writeError(c, http.StatusTooManyRequests, msgMaxStreams)
			return
		}
	} else {
		// Unauthenticated local-media stream: enforce per-IP limit before serving.
		// The proxy and extractor branches each perform their own CanStartStream
		// check above; the local path needs its own because no pre-flight guard
		// was in place and streaming.Stream() does not check the limit internally.
		userID = "ip:" + c.ClientIP()
		if limit := streamCfg.UnauthStreamLimit; limit > 0 {
			if !h.streaming.CanStartStream(userID, limit) {
				writeError(c, http.StatusTooManyRequests, msgMaxStreamsConn)
				return
			}
		}
	}

	req := streaming.StreamRequest{
		Path:        absPath,
		MediaID:     id,
		Quality:     c.Query("quality"),
		UserID:      userID,
		SessionID:   sessionID,
		IPAddress:   c.ClientIP(),
		UserAgent:   c.Request.UserAgent(),
		RangeHeader: c.Request.Header.Get("Range"),
	}

	isInitialRequest := req.RangeHeader == "" || strings.HasPrefix(req.RangeHeader, "bytes=0-")
	if isInitialRequest && h.tryRecordView(userID, id) {
		if h.analytics != nil {
			h.analytics.TrackView(c.Request.Context(), analytics.ViewParams{
				MediaID:   id,
				UserID:    userID,
				SessionID: sessionID,
				IPAddress: req.IPAddress,
				UserAgent: req.UserAgent,
			})
		}

		if h.suggestions != nil && userID != "" && localItem != nil {
			h.suggestions.RecordView(userID, absPath, localItem.Category, string(localItem.Type), localItem.Duration)
		}

		if err := h.media.IncrementViews(c.Request.Context(), absPath); err != nil {
			h.log.Warn("Failed to increment view count for %s: %v", absPath, err)
		}
	}

	if err := h.streaming.Stream(c.Writer, c.Request, req); err != nil {
		if c.Writer.Written() || isClientDisconnect(err) {
			return
		}
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
	if !cfg.Download.Enabled {
		writeError(c, http.StatusForbidden, "Downloads are disabled")
		return
	}
	session := getSession(c)

	if cfg.Download.RequireAuth && session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	// When RequireAuth is false, anonymous downloads are allowed; when true, CanDownload is checked for session users.
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

	id := strings.TrimSpace(c.Query("id"))
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
					writeError(c, http.StatusForbidden, msgMatureContent)
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
		if !h.media.IsReady() {
			c.Header(headerRetryAfter, "3")
			writeError(c, http.StatusServiceUnavailable, msgInitializing)
		} else {
			writeError(c, http.StatusNotFound, errMediaNotFound)
		}
		return
	}
	absPath := localItem.Path

	if !h.checkMatureAccess(c, localItem.IsMature) {
		return
	}

	// Track download for analytics
	if h.analytics != nil {
		userID := ""
		sessionID := ""
		if session != nil {
			userID = session.UserID
			sessionID = session.ID
		}
		h.analytics.TrackDownload(c.Request.Context(), analytics.ViewParams{
			MediaID: id, UserID: userID, SessionID: sessionID,
			IPAddress: c.ClientIP(), UserAgent: c.Request.UserAgent(),
		})
	}

	if err := h.streaming.Download(c.Writer, c.Request, absPath); err != nil {
		if c.Writer.Written() || isClientDisconnect(err) {
			if isClientDisconnect(err) {
				h.log.Debug("Download canceled by client: %v", err)
			}
			return
		}
		switch {
		case errors.Is(err, streaming.ErrFileNotFound):
			writeError(c, http.StatusNotFound, errFileNotFound)
		case errors.Is(err, streaming.ErrFileTooLarge):
			writeError(c, http.StatusRequestEntityTooLarge, "File exceeds maximum download size")
		default:
			h.log.Error("Download error: %v", err)
			writeError(c, http.StatusInternalServerError, "Download error")
		}
	}
}

// GetBatchPlaybackPositions returns playback positions for multiple media IDs.
// Query param: ids=id1,id2,... (max 100)
func (h *Handler) GetBatchPlaybackPositions(c *gin.Context) {
	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	raw := c.Query("ids")
	if raw == "" {
		writeSuccess(c, map[string]any{"positions": map[string]float64{}})
		return
	}

	ids := strings.Split(raw, ",")
	if len(ids) > 100 {
		ids = ids[:100]
	}
	// Trim whitespace from each ID.
	for i, id := range ids {
		ids[i] = strings.TrimSpace(id)
	}

	positions := h.media.BatchGetPlaybackPositions(c.Request.Context(), ids, session.UserID)
	writeSuccess(c, map[string]any{"positions": positions})
}

// GetBatchMedia returns media items for multiple IDs in a single request.
// Query param: ids=id1,id2,... (max 100)
func (h *Handler) GetBatchMedia(c *gin.Context) {
	raw := c.Query("ids")
	if raw == "" {
		writeSuccess(c, map[string]any{"items": map[string]*models.MediaItem{}})
		return
	}

	ids := strings.Split(raw, ",")
	if len(ids) > 100 {
		ids = ids[:100]
	}

	canViewMature := h.canViewMatureContent(c)
	items := make(map[string]*models.MediaItem, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		item, err := h.media.GetMediaByID(id)
		if err != nil {
			continue
		}
		if item.IsMature && !canViewMature {
			continue
		}
		items[id] = item
	}

	writeSuccess(c, map[string]any{"items": items})
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
	if !BindJSON(c, &req, "") {
		return
	}

	if req.Position < 0 {
		req.Position = 0
	}
	// Cap position against the reported duration to prevent users from instantly
	// marking items as 100% complete via a forged position value.  Also apply a
	// hard maximum (one week in seconds) as a sanity guard independent of duration.
	const maxPositionSecs = 7 * 24 * 3600 // 7 days — no legitimate media is longer
	if req.Duration > 0 && req.Position > req.Duration {
		req.Position = req.Duration
	}
	if req.Position > maxPositionSecs {
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
		var progress float64
		if req.Duration > 0 {
			progress = req.Position / req.Duration
		}
		if err := h.media.UpdatePlaybackPosition(c.Request.Context(), mediaPath, userID, req.Position, req.Duration, progress); err != nil {
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
			if req.Duration > 0 {
				item.Progress = req.Position / req.Duration
			}
			item.Completed = item.Progress >= 0.9
			if err := h.auth.AddToWatchHistory(c.Request.Context(), username, item); err != nil {
				h.log.Warn("Watch history update failed for media %s: %v", req.ID, err)
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

// canViewMatureContent reports whether the current request's user is authorized
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
