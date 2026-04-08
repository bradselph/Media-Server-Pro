package handlers

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/analytics"
	"media-server-pro/internal/auth"
	"media-server-pro/pkg/models"
)

// Login authenticates a user or admin using the same endpoint
func (h *Handler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	authReq := &auth.AuthRequest{
		Username:  req.Username,
		Password:  req.Password,
		IPAddress: c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	}
	adminSession, adminErr := h.auth.AdminAuthenticate(c.Request.Context(), authReq)

	if adminErr != nil {
		if errors.Is(adminErr, auth.ErrAccountLocked) {
			writeError(c, http.StatusTooManyRequests, "Too many failed login attempts. Please try again later.")
			return
		}
		if errors.Is(adminErr, auth.ErrAdminWrongPassword) {
			writeError(c, http.StatusUnauthorized, "Invalid credentials")
			return
		}
		if !errors.Is(adminErr, auth.ErrNotAdminUsername) {
			writeError(c, http.StatusUnauthorized, "Invalid credentials")
			return
		}
	} else {
		session, sessErr := h.auth.CreateSessionForUser(c.Request.Context(), &auth.CreateSessionParams{
			Username:  adminSession.Username,
			IPAddress: c.ClientIP(),
			UserAgent: c.Request.UserAgent(),
		})
		if sessErr != nil {
			h.log.Error("Failed to create admin session: %v", sessErr)
			writeError(c, http.StatusInternalServerError, "Failed to create session")
			return
		}

		http.SetCookie(c.Writer, &http.Cookie{
			Name:     "session_id",
			Value:    session.ID,
			Path:     "/",
			Expires:  session.ExpiresAt,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   isSecureRequest(c.Request),
		})
		if h.analytics != nil {
			h.analytics.TrackTrafficEvent(c.Request.Context(), analytics.TrafficEventParams{
				Type: analytics.EventLogin, UserID: session.UserID, SessionID: session.ID,
				IPAddress: c.ClientIP(), UserAgent: c.Request.UserAgent(),
				Data: map[string]interface{}{"username": session.Username, "role": string(session.Role)},
			})
		}
		writeSuccess(c, map[string]interface{}{
			"session_id": session.ID,
			"username":   session.Username,
			"role":       session.Role,
			"is_admin":   session.Role == models.RoleAdmin,
			"expires_at": session.ExpiresAt,
		})
		return
	}

	session, err := h.auth.Authenticate(c.Request.Context(), authReq)
	if err != nil {
		// Track failed login attempt
		if h.analytics != nil {
			reason := "invalid_credentials"
			if errors.Is(err, auth.ErrAccountLocked) {
				reason = "account_locked"
			}
			h.analytics.TrackTrafficEvent(c.Request.Context(), analytics.TrafficEventParams{
				Type: analytics.EventLoginFailed, IPAddress: c.ClientIP(), UserAgent: c.Request.UserAgent(),
				Data: map[string]interface{}{"username": req.Username, "reason": reason},
			})
		}
		if errors.Is(err, auth.ErrAccountLocked) {
			writeError(c, http.StatusTooManyRequests, "Too many failed login attempts. Please try again later.")
			return
		}
		writeError(c, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "session_id",
		Value:    session.ID,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   isSecureRequest(c.Request),
	})

	// Track successful login for traffic analytics
	if h.analytics != nil {
		h.analytics.TrackTrafficEvent(c.Request.Context(), analytics.TrafficEventParams{
			Type: analytics.EventLogin, UserID: session.UserID, SessionID: session.ID,
			IPAddress: c.ClientIP(), UserAgent: c.Request.UserAgent(),
			Data: map[string]interface{}{"username": session.Username, "role": string(session.Role)},
		})
	}

	writeSuccess(c, map[string]interface{}{
		"session_id": session.ID,
		"username":   session.Username,
		"role":       session.Role,
		"is_admin":   session.Role == models.RoleAdmin,
		"expires_at": session.ExpiresAt,
	})
}

// Logout invalidates a session (both regular and admin)
func (h *Handler) Logout(c *gin.Context) {
	cookie, err := c.Request.Cookie("session_id")
	if err == nil {
		// Try regular session first; fall back to admin session
		if logoutErr := h.auth.Logout(c.Request.Context(), cookie.Value); logoutErr != nil {
			if adminErr := h.auth.LogoutAdmin(c.Request.Context(), cookie.Value); adminErr != nil {
				h.log.Warn("Failed to logout session: %v", logoutErr)
			}
		}
	}

	// Track logout for traffic analytics
	if h.analytics != nil {
		sess := getSession(c)
		var uid, sid string
		if sess != nil {
			uid = sess.UserID
			sid = sess.ID
		}
		h.analytics.TrackTrafficEvent(c.Request.Context(), analytics.TrafficEventParams{
			Type: analytics.EventLogout, UserID: uid, SessionID: sid,
			IPAddress: c.ClientIP(), UserAgent: c.Request.UserAgent(),
		})
	}

	clearSessionCookie(c.Writer, c.Request)
	writeSuccess(c, nil)
}

// CheckSession returns the current session status
func (h *Handler) CheckSession(c *gin.Context) {
	cfg := h.media.GetConfig()
	allowGuests := cfg.Auth.AllowGuests

	user := getUser(c)
	if user == nil {
		writeSuccess(c, map[string]interface{}{
			"authenticated": false,
			"allow_guests":  allowGuests,
		})
		return
	}

	writeSuccess(c, map[string]interface{}{
		"authenticated": true,
		"allow_guests":  allowGuests,
		"user":          user,
	})
}

// Register creates a new user account
func (h *Handler) Register(c *gin.Context) {
	cfg := h.config.Get()
	if !cfg.Auth.AllowRegistration {
		writeError(c, http.StatusForbidden, "Registration is disabled")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)
	if len(req.Username) < 3 || len(req.Username) > 64 {
		writeError(c, http.StatusBadRequest, "Username must be between 3 and 64 characters")
		return
	}
	if len(req.Password) < 8 {
		writeError(c, http.StatusBadRequest, "Password must be at least 8 characters")
		return
	}
	for _, ch := range req.Username {
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '_' && ch != '-' {
			writeError(c, http.StatusBadRequest, "Username may only contain letters, numbers, underscores, and hyphens")
			return
		}
	}
	if req.Email != "" {
		if _, parseErr := mail.ParseAddress(req.Email); parseErr != nil {
			writeError(c, http.StatusBadRequest, "Invalid email address")
			return
		}
	}

	user, err := h.auth.CreateUser(c.Request.Context(), auth.CreateUserParams{
		Username: req.Username,
		Password: req.Password,
		Email:    req.Email,
		UserType: "standard",
		Role:     models.RoleViewer,
	})
	if err != nil {
		if errors.Is(err, auth.ErrUserExists) {
			writeError(c, http.StatusConflict, "Username is already taken")
			return
		}
		h.log.Error("Failed to create user %s: %v", req.Username, err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	session, authErr := h.auth.CreateSessionForUser(c.Request.Context(), &auth.CreateSessionParams{
		Username:  req.Username,
		IPAddress: c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	})
	if authErr != nil {
		h.log.Error("Failed to create session for new user %s: %v", req.Username, authErr)
		writeError(c, http.StatusInternalServerError, "Account created but login failed")
		return
	}

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "session_id",
		Value:    session.ID,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   isSecureRequest(c.Request),
	})

	// Track registration for traffic analytics
	if h.analytics != nil {
		h.analytics.TrackTrafficEvent(c.Request.Context(), analytics.TrafficEventParams{
			Type: analytics.EventRegister, UserID: session.UserID, SessionID: session.ID,
			IPAddress: c.ClientIP(), UserAgent: c.Request.UserAgent(),
			Data: map[string]interface{}{"username": req.Username},
		})
	}

	writeSuccess(c, user)
}

// GetPreferences returns the current user's preferences
func (h *Handler) GetPreferences(c *gin.Context) {
	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	var user *models.User
	if session.Role == models.RoleAdmin {
		if dbUser, err := h.auth.GetUser(c.Request.Context(), session.Username); err == nil {
			user = dbUser
		}
	}

	if user == nil {
		user = getUser(c)
	}
	if user == nil {
		var err error
		user, err = h.auth.GetUserByID(c.Request.Context(), session.UserID)
		if err != nil {
			writeError(c, http.StatusNotFound, errUserNotFound)
			return
		}
	}

	writeSuccess(c, user.Preferences)
}

// UpdatePreferences updates the current user's preferences
func (h *Handler) UpdatePreferences(c *gin.Context) {
	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	var incoming map[string]interface{}
	if err := json.NewDecoder(c.Request.Body).Decode(&incoming); err != nil {
		h.log.Error("Failed to decode preferences JSON for user %s: %v", session.Username, err)
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	if session.Role == models.RoleAdmin {
		if _, err := h.auth.GetUser(c.Request.Context(), session.Username); err != nil {
			randomPassword, pwdErr := h.auth.GenerateSecurePassword(32)
			if pwdErr != nil {
				h.log.Error("Failed to generate password for admin user record: %v", pwdErr)
				randomPassword = "FALLBACK_UNUSED_PASSWORD_" + generateRandomString(24)
			}
			if _, createErr := h.auth.CreateUser(c.Request.Context(), auth.CreateUserParams{
				Username: session.Username,
				Password: randomPassword,
				Email:    "",
				UserType: "admin",
				Role:     models.RoleAdmin,
			}); createErr != nil {
				h.log.Warn("Could not create admin user record for preferences: %v", createErr)
				writeError(c, http.StatusServiceUnavailable, "User record could not be created. Please try again later.")
				return
			}
		}
	}

	user, err := h.auth.GetUser(c.Request.Context(), session.Username)
	if err != nil || user == nil {
		writeError(c, http.StatusNotFound, errUserNotFound)
		return
	}
	prefs := user.Preferences

	if v, ok := incoming["theme"].(string); ok {
		prefs.Theme = v
	}
	if v, ok := incoming["view_mode"].(string); ok {
		prefs.ViewMode = v
	}
	if v, ok := incoming["default_quality"].(string); ok {
		prefs.DefaultQuality = v
	}
	if v, ok := incoming["auto_play"].(bool); ok {
		prefs.AutoPlay = v
	}
	if v, ok := incoming["autoplay"].(bool); ok {
		prefs.AutoPlay = v
	}
	if v, ok := incoming["playback_speed"].(float64); ok {
		if v < 0.25 {
			v = 0.25
		} else if v > 4.0 {
			v = 4.0
		}
		prefs.PlaybackSpeed = v
	}
	if v, ok := incoming["volume"].(float64); ok {
		if v < 0 {
			v = 0
		} else if v > 1.0 {
			v = 1.0
		}
		prefs.Volume = v
	}
	if showMature, ok := incoming["show_mature"].(bool); ok {
		prefs.ShowMature = showMature
		prefs.MaturePreferenceSet = true
	}
	if v, ok := incoming["language"].(string); ok {
		prefs.Language = v
	}
	if v, ok := incoming["equalizer_preset"].(string); ok {
		prefs.EqualizerPreset = v
	} else if v, ok := incoming["equalizer_bands"].(string); ok {
		prefs.EqualizerPreset = v
	}
	if v, ok := incoming["resume_playback"].(bool); ok {
		prefs.ResumePlayback = v
	}
	if v, ok := incoming["show_analytics"].(bool); ok {
		prefs.ShowAnalytics = v
	}
	if v, ok := incoming["items_per_page"].(float64); ok {
		n := int(v)
		if n < 1 {
			n = 1
		} else if n > 200 {
			n = 200
		}
		prefs.ItemsPerPage = n
	}
	if v, ok := incoming["sort_by"].(string); ok {
		prefs.SortBy = v
	}
	if v, ok := incoming["sort_order"].(string); ok {
		prefs.SortOrder = v
	}
	if v, ok := incoming["filter_category"].(string); ok {
		prefs.FilterCategory = v
	}
	if v, ok := incoming["filter_media_type"].(string); ok {
		prefs.FilterMediaType = v
	}
	if v, ok := incoming["custom_eq_presets"].(map[string]interface{}); ok {
		prefs.CustomEQPresets = v
	}
	if v, ok := incoming["show_continue_watching"].(bool); ok {
		prefs.ShowContinueWatching = v
	}
	if v, ok := incoming["show_recommended"].(bool); ok {
		prefs.ShowRecommended = v
	}
	if v, ok := incoming["show_trending"].(bool); ok {
		prefs.ShowTrending = v
	}

	h.log.Debug("Updating preferences for user %s: show_mature=%v, mature_preference_set=%v", session.Username, prefs.ShowMature, prefs.MaturePreferenceSet)

	if err := h.auth.UpdateUserPreferences(c.Request.Context(), session.Username, prefs); err != nil {
		h.log.Error("Failed to update preferences for user %s: %v", session.Username, err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(c, prefs)
}

// GetWatchHistory returns the current user's watch history
func (h *Handler) GetWatchHistory(c *gin.Context) {
	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	user := getUser(c)
	if user == nil {
		var err error
		user, err = h.auth.GetUserByID(c.Request.Context(), session.UserID)
		if err != nil {
			writeError(c, http.StatusNotFound, errUserNotFound)
			return
		}
	}

	history := user.WatchHistory
	if history == nil {
		history = []models.WatchHistoryItem{}
	}

	// Enrich entries that have a missing media name with a single batch lookup
	// instead of one lock acquisition per item. TrackPlayback populates MediaName
	// at write time, so this fallback path is only hit for old history entries.
	var missingIDs []string
	for i := range history {
		if history[i].MediaName == "" {
			missingIDs = append(missingIDs, history[i].MediaID)
		}
	}
	if len(missingIDs) > 0 {
		names := h.media.GetMediaNamesByIDs(missingIDs)
		for i := range history {
			if history[i].MediaName == "" {
				if name, ok := names[history[i].MediaID]; ok {
					history[i].MediaName = name
				}
			}
		}
	}

	if idFilter := c.Query("id"); idFilter != "" {
		var matched []models.WatchHistoryItem
		for _, item := range history {
			if item.MediaID == idFilter {
				matched = append(matched, item)
				break
			}
		}
		if matched == nil {
			matched = []models.WatchHistoryItem{}
		}
		writeSuccess(c, matched)
		return
	}

	if completedFilter := c.Query("completed"); completedFilter != "" {
		var filtered []models.WatchHistoryItem
		want := completedFilter == "true"
		for _, item := range history {
			if item.Completed == want {
				filtered = append(filtered, item)
			}
		}
		if filtered == nil {
			filtered = []models.WatchHistoryItem{}
		}
		history = filtered
	}

	if limitStr := c.Query("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 && limit < len(history) {
			history = history[:limit]
		}
	}

	writeSuccess(c, history)
}

// ClearWatchHistory clears the user's watch history.
func (h *Handler) ClearWatchHistory(c *gin.Context) {
	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	if mediaID := c.Query("id"); mediaID != "" {
		// Resolve ID to path for internal operations — fall back to receiver media, then
		// to the stored path in the user's own watch history for items whose media has
		// since been deleted from the library (allows users to clean up stale entries).
		var mediaPath string
		item, err := h.media.GetMediaByID(mediaID)
		if err != nil {
			// Check receiver media
			if h.receiver != nil {
				if ri := h.receiver.GetMediaItem(mediaID); ri != nil {
					mediaPath = "receiver:" + mediaID
				}
			}
			// Fall back to the path stored in watch history for deleted media
			if mediaPath == "" {
				if history, herr := h.auth.GetWatchHistory(session.Username); herr == nil {
					for _, hi := range history {
						if hi.MediaID == mediaID {
							mediaPath = hi.MediaPath
							break
						}
					}
				}
			}
			if mediaPath == "" {
				writeError(c, http.StatusNotFound, errMediaNotFound)
				return
			}
		} else {
			mediaPath = item.Path
		}
		if err := h.auth.RemoveWatchHistoryItem(c.Request.Context(), session.Username, mediaPath); err != nil {
			h.log.Error("%v", err)
			writeError(c, http.StatusInternalServerError, "Internal server error")
			return
		}
		// Only clear local playback positions (receiver items have no server-side position store)
		if item != nil {
			h.media.ClearPlaybackPosition(c.Request.Context(), mediaPath, session.UserID)
		}
		writeSuccess(c, map[string]string{"status": "removed"})
		return
	}

	if err := h.auth.ClearWatchHistory(c.Request.Context(), session.Username); err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}
	h.media.ClearAllPlaybackPositions(session.UserID)

	writeSuccess(c, map[string]string{"status": "cleared"})
}

// GetPermissions returns the current user's permissions and capabilities
func (h *Handler) GetPermissions(c *gin.Context) {
	session := getSession(c)

	if session == nil {
		writeSuccess(c, map[string]interface{}{
			"authenticated":         false,
			"show_mature":           false,
			"mature_preference_set": false,
			"capabilities": map[string]bool{
				"canUpload":          false,
				"canDownload":        false,
				"canCreatePlaylists": false,
				"canViewMature":      false,
				"canStream":          false,
				"canDelete":          false,
				"canManage":          false,
			},
		})
		return
	}

	user, err := h.auth.GetUserByID(c.Request.Context(), session.UserID)
	if err != nil {
		writeError(c, http.StatusNotFound, errUserNotFound)
		return
	}

	writeSuccess(c, map[string]interface{}{
		"authenticated":         true,
		"username":              user.Username,
		"role":                  user.Role,
		"user_type":             user.Type,
		"show_mature":           user.Preferences.ShowMature,
		"mature_preference_set": user.Preferences.MaturePreferenceSet,
		"capabilities": map[string]bool{
			"canUpload":          user.Permissions.CanUpload,
			"canDownload":        user.Permissions.CanDownload,
			"canCreatePlaylists": user.Permissions.CanCreatePlaylists,
			"canViewMature":      user.Permissions.CanViewMature,
			"canStream":          user.Permissions.CanStream,
			"canDelete":          user.Permissions.CanDelete,
			"canManage":          user.Permissions.CanManage,
		},
		// storage_quota is in bytes; GetStorageUsage's quota_gb is in GB (already divided).
		"limits": map[string]interface{}{
			"storage_quota":      h.getUserStorageQuota(user.Type),
			"concurrent_streams": h.getUserStreamLimit(user.Type),
		},
	})
}

// ChangePassword allows a user to change their own password.
func (h *Handler) ChangePassword(c *gin.Context) {
	user := getUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		writeError(c, http.StatusBadRequest, "Current and new password required")
		return
	}

	if len(req.NewPassword) < 8 {
		writeError(c, http.StatusBadRequest, "New password must be at least 8 characters")
		return
	}

	if h.auth.VerifyPassword(c.Request.Context(), user.Username, req.CurrentPassword) != nil {
		writeError(c, http.StatusUnauthorized, "Current password is incorrect")
		return
	}

	if err := h.auth.SetPassword(c.Request.Context(), user.Username, req.NewPassword); err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(c, map[string]string{"status": "password_changed"})
}

// DeleteAccount allows an authenticated user to permanently delete their own account.
func (h *Handler) DeleteAccount(c *gin.Context) {
	user := getUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	if user.Role == "admin" {
		writeError(c, http.StatusForbidden, "Admin accounts cannot be deleted via this endpoint")
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	if req.Password == "" {
		writeError(c, http.StatusBadRequest, "Password confirmation required")
		return
	}

	if h.auth.VerifyPassword(c.Request.Context(), user.Username, req.Password) != nil {
		writeError(c, http.StatusUnauthorized, "Incorrect password")
		return
	}

	// Delete the account first — if this fails, the user remains logged in and can retry.
	if err := h.auth.DeleteUser(c.Request.Context(), user.Username); err != nil {
		h.log.Error("Failed to delete account: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to delete account")
		return
	}

	// Invalidate the session only after successful deletion.
	session := getSession(c)
	if session != nil {
		if err := h.auth.Logout(c.Request.Context(), session.ID); err != nil {
			h.log.Warn("Failed to invalidate session after account deletion for %s: %v", user.Username, err)
		}
		clearSessionCookie(c.Writer, c.Request)
	}

	h.log.Info("User %s deleted their account", user.Username)
	writeSuccess(c, map[string]string{"status": "account_deleted", "message": "Your account has been permanently deleted"})
}

// ExportWatchHistory exports the user's watch history as a CSV file.
// The CSV is buffered in memory first so that errors during generation
// produce a 500 response instead of a truncated file. A trailer comment
// is appended on send failure so consumers can detect incomplete exports.
func (h *Handler) ExportWatchHistory(c *gin.Context) {
	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	user, err := h.auth.GetUserByID(c.Request.Context(), session.UserID)
	if err != nil {
		writeError(c, http.StatusNotFound, errUserNotFound)
		return
	}

	history := user.WatchHistory
	if history == nil {
		history = []models.WatchHistoryItem{}
	}

	// Buffer the entire CSV so we can return 500 on generation errors
	// instead of sending a truncated file.
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"media_name", "media_id", "watched_at", "position_seconds", "duration_seconds", "progress_percent", "completed"}); err != nil {
		h.log.Error("CSV header write failed: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to generate export")
		return
	}
	for _, item := range history {
		completed := "no"
		if item.Completed {
			completed = "yes"
		}
		if err := w.Write([]string{
			item.MediaName,
			item.MediaID,
			item.WatchedAt.Format("2006-01-02T15:04:05Z"),
			fmt.Sprintf("%.1f", item.Position),
			fmt.Sprintf("%.1f", item.Duration),
			fmt.Sprintf("%.1f", item.Progress*100),
			completed,
		}); err != nil {
			h.log.Error("CSV row write failed: %v", err)
			writeError(c, http.StatusInternalServerError, "Failed to generate export")
			return
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		h.log.Error("CSV flush failed for user %s: %v", user.Username, err)
		writeError(c, http.StatusInternalServerError, "Failed to generate export")
		return
	}

	// Buffer is complete — send it
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="watch_history_%s.csv"`, user.Username))
	c.Header("Content-Length", strconv.Itoa(buf.Len()))
	if _, err := c.Writer.Write(buf.Bytes()); err != nil {
		// Headers already sent — append trailer comment so the consumer can detect truncation.
		h.log.Error("CSV send failed for user %s: %v", user.Username, err)
		_, _ = c.Writer.Write([]byte("\n# ERROR: export incomplete\n"))
	}
}
