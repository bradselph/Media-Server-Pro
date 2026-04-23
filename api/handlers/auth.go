package handlers

import (
	"bytes"
	cryptorand "crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/analytics"
	"media-server-pro/internal/auth"
	"media-server-pro/pkg/models"
)

const regTokenTTL = 15 * time.Minute

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
			writeError(c, http.StatusUnauthorized, errInvalidCredentials)
			return
		}
		if !errors.Is(adminErr, auth.ErrNotAdminUsername) {
			writeError(c, http.StatusUnauthorized, errInvalidCredentials)
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
				Data: map[string]any{"username": session.Username, "role": string(session.Role)},
			})
		}
		writeSuccess(c, map[string]any{
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
				Data: map[string]any{"username": req.Username, "reason": reason},
			})
		}
		if errors.Is(err, auth.ErrAccountLocked) {
			writeError(c, http.StatusTooManyRequests, "Too many failed login attempts. Please try again later.")
			return
		}
		writeError(c, http.StatusUnauthorized, errInvalidCredentials)
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
			Data: map[string]any{"username": session.Username, "role": string(session.Role)},
		})
	}

	writeSuccess(c, map[string]any{
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
				h.log.Warn("Failed to logout session (regular: %v, admin: %v)", logoutErr, adminErr)
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
		writeSuccess(c, map[string]any{
			"authenticated": false,
			"allow_guests":  allowGuests,
		})
		return
	}

	writeSuccess(c, map[string]any{
		"authenticated": true,
		"allow_guests":  allowGuests,
		"user":          user,
	})
}

// GetRegistrationToken issues a single-use server-signed token required to call Register.
// The frontend fetches this when the signup page loads; curl scripts without it are rejected.
func (h *Handler) GetRegistrationToken(c *gin.Context) {
	if !h.config.Get().Auth.AllowRegistration {
		writeError(c, http.StatusForbidden, "Registration is disabled")
		return
	}
	raw := make([]byte, 32)
	if _, err := cryptorand.Read(raw); err != nil {
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}
	token := hex.EncodeToString(raw)
	h.regTokens.Store(token, time.Now())
	writeSuccess(c, map[string]string{"token": token})
}

// Register creates a new user account.
// A valid registration token (obtained from GetRegistrationToken) is required.
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
		Token    string `json:"token"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	// Validate the server-issued registration token (single-use, 15-min TTL).
	issued, ok := h.regTokens.LoadAndDelete(req.Token)
	if !ok || req.Token == "" || time.Since(issued.(time.Time)) > regTokenTTL {
		writeError(c, http.StatusForbidden, "Invalid or expired registration token")
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
		writeError(c, http.StatusInternalServerError, errInternalServer)
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
			Data: map[string]any{"username": req.Username},
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

	var incoming map[string]any
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
			}); createErr != nil && !errors.Is(createErr, auth.ErrUserExists) {
				// ErrUserExists means a concurrent request already created the record —
				// that's fine, proceed to read it. Any other error is a real failure.
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
	applyPreferencesPatch(&prefs, incoming)

	h.log.Debug("Updating preferences for user %s: show_mature=%v, mature_preference_set=%v", session.Username, prefs.ShowMature, prefs.MaturePreferenceSet)

	if err := h.auth.UpdateUserPreferences(c.Request.Context(), session.Username, prefs); err != nil {
		h.log.Error("Failed to update preferences for user %s: %v", session.Username, err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}

	// Validate normalises the stored struct (clamping, enum defaults, etc.) so the
	// response reflects exactly what was committed, preventing a discrepancy that
	// would make the client think one value was saved when another was stored.
	prefs.Validate()
	writeSuccess(c, prefs)
}

func setStringPref(m map[string]any, key string, dst *string) {
	if v, ok := m[key].(string); ok {
		*dst = v
	}
}

func setBoolPref(m map[string]any, key string, dst *bool) {
	if v, ok := m[key].(bool); ok {
		*dst = v
	}
}

func setClampedFloatPref(m map[string]any, key string, dst *float64, lo, hi float64) {
	v, ok := m[key].(float64)
	if !ok {
		return
	}
	if v < lo {
		v = lo
	} else if v > hi {
		v = hi
	}
	*dst = v
}

func setClampedIntPref(m map[string]any, key string, dst *int, lo, hi int) {
	v, ok := m[key].(float64)
	if !ok {
		return
	}
	n := int(v)
	if n < lo {
		n = lo
	} else if n > hi {
		n = hi
	}
	*dst = n
}

// applyPreferencesPatch overlays caller-supplied fields onto prefs. Unknown
// keys or type mismatches are ignored; numeric fields are clamped to their
// accepted ranges before the call returns.
func applyPreferencesPatch(prefs *models.UserPreferences, m map[string]any) {
	setStringPref(m, "theme", &prefs.Theme)
	setStringPref(m, "view_mode", &prefs.ViewMode)
	setStringPref(m, "default_quality", &prefs.DefaultQuality)
	setBoolPref(m, "auto_play", &prefs.AutoPlay)
	setBoolPref(m, "autoplay", &prefs.AutoPlay)
	setClampedFloatPref(m, "playback_speed", &prefs.PlaybackSpeed, 0.25, 4.0)
	setClampedFloatPref(m, "volume", &prefs.Volume, 0, 1.0)
	if v, ok := m["show_mature"].(bool); ok {
		prefs.ShowMature = v
		prefs.MaturePreferenceSet = true
	}
	setStringPref(m, "language", &prefs.Language)
	if v, ok := m["equalizer_preset"].(string); ok {
		prefs.EqualizerPreset = v
	} else if v, ok := m["equalizer_bands"].(string); ok {
		prefs.EqualizerPreset = v
	}
	setBoolPref(m, "resume_playback", &prefs.ResumePlayback)
	setBoolPref(m, "show_analytics", &prefs.ShowAnalytics)
	setClampedIntPref(m, "items_per_page", &prefs.ItemsPerPage, 1, 200)
	setStringPref(m, "sort_by", &prefs.SortBy)
	setStringPref(m, "sort_order", &prefs.SortOrder)
	setStringPref(m, "filter_category", &prefs.FilterCategory)
	setStringPref(m, "filter_media_type", &prefs.FilterMediaType)
	if v, ok := m["custom_eq_presets"].(map[string]any); ok {
		prefs.CustomEQPresets = v
	}
	setBoolPref(m, "show_continue_watching", &prefs.ShowContinueWatching)
	setBoolPref(m, "show_recommended", &prefs.ShowRecommended)
	setBoolPref(m, "show_trending", &prefs.ShowTrending)
	setClampedIntPref(m, "skip_interval", &prefs.SkipInterval, 1, 300)
	setBoolPref(m, "shuffle_enabled", &prefs.ShuffleEnabled)
	setBoolPref(m, "show_buffer_bar", &prefs.ShowBufferBar)
	setBoolPref(m, "download_prompt", &prefs.DownloadPrompt)
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

	// Copy the slice before enriching — user is the raw cached *models.User pointer
	// and mutating history[i].MediaName on the shared backing array races with
	// AddToWatchHistory (writes to user.WatchHistory[i] under usersMu.Lock).
	history := make([]models.WatchHistoryItem, len(user.WatchHistory))
	copy(history, user.WatchHistory)

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
				if h.receiver.GetMediaItem(mediaID) != nil {
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
			writeError(c, http.StatusInternalServerError, errInternalServer)
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
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}
	h.media.ClearAllPlaybackPositions(session.UserID)

	writeSuccess(c, map[string]string{"status": "cleared"})
}

// GetPermissions returns the current user's permissions and capabilities
func (h *Handler) GetPermissions(c *gin.Context) {
	session := getSession(c)

	if session == nil {
		writeSuccess(c, map[string]any{
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

	caps := map[string]bool{
		"canUpload":          user.Permissions.CanUpload,
		"canDownload":        user.Permissions.CanDownload,
		"canCreatePlaylists": user.Permissions.CanCreatePlaylists,
		"canViewMature":      user.Permissions.CanViewMature,
		"canStream":          user.Permissions.CanStream,
		// Always present so frontend can read all 7 flags without nil-checking.
		// Non-admin users always get false; admins get their actual permission value.
		"canDelete": false,
		"canManage": false,
	}
	if user.Role == models.RoleAdmin {
		caps["canDelete"] = user.Permissions.CanDelete
		caps["canManage"] = user.Permissions.CanManage
	}
	writeSuccess(c, map[string]any{
		"authenticated":         true,
		"username":              user.Username,
		"role":                  user.Role,
		"user_type":             user.Type,
		"show_mature":           user.Preferences.ShowMature,
		"mature_preference_set": user.Preferences.MaturePreferenceSet,
		"capabilities":          caps,
		// storage_quota is in bytes; GetStorageUsage's quota_gb is in GB (already divided).
		"limits": map[string]any{
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

	if err := h.auth.UpdatePassword(c.Request.Context(), user.Username, req.CurrentPassword, req.NewPassword); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(c, http.StatusUnauthorized, "Current password is incorrect")
			return
		}
		h.log.Error("Password change failed for %s: %v", user.Username, err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
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

	// Clear the session cookie. DeleteUser already evicts all sessions from cache
	// and DB via evictSessionsForUser, so an explicit Logout call is unnecessary
	// and would always fail with ErrSessionNotFound.
	clearSessionCookie(c.Writer, c.Request)

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

	// Copy the slice — user is the raw cached pointer; concurrent writes to
	// user.WatchHistory via AddToWatchHistory race with reads here.
	history := make([]models.WatchHistoryItem, len(user.WatchHistory))
	copy(history, user.WatchHistory)

	// Buffer the entire CSV so we can return 500 on generation errors
	// instead of sending a truncated file.
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"media_name", "media_id", "watched_at", "position_seconds", "duration_seconds", "progress_percent", "completed"}); err != nil {
		h.log.Error("CSV header write failed: %v", err)
		writeError(c, http.StatusInternalServerError, msgFailedExport)
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
			writeError(c, http.StatusInternalServerError, msgFailedExport)
			return
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		h.log.Error("CSV flush failed for user %s: %v", user.Username, err)
		writeError(c, http.StatusInternalServerError, msgFailedExport)
		return
	}

	// Buffer is complete — send it.
	// Content-Length is set so HTTP clients detect short reads if the connection drops.
	// Do not append a trailer on write failure — the Content-Length mismatch is
	// the reliable signal, and appending text to a partially-written CSV corrupts it.
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="watch_history_%s.csv"`, user.Username))
	c.Header("Content-Length", strconv.Itoa(buf.Len()))
	if _, err := c.Writer.Write(buf.Bytes()); err != nil {
		h.log.Error("CSV send failed for user %s: %v", user.Username, err)
	}
}
