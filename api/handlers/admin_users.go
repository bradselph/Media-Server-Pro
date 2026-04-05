package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/auth"
	"media-server-pro/pkg/models"
)

// AdminListUsers returns all users
func (h *Handler) AdminListUsers(c *gin.Context) {
	users := h.auth.ListUsers(c.Request.Context())
	writeSuccess(c, users)
}

// validUsername checks that name is 3–64 chars of [a-zA-Z0-9_-].
func validUsername(name string) error {
	if len(name) < 3 || len(name) > 64 {
		return fmt.Errorf("username must be between 3 and 64 characters")
	}
	for _, ch := range name {
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '_' && ch != '-' {
			return fmt.Errorf("username may only contain letters, numbers, underscores, and hyphens")
		}
	}
	return nil
}

// AdminCreateUser creates a user
func (h *Handler) AdminCreateUser(c *gin.Context) {
	var req struct {
		Username string          `json:"username"`
		Password string          `json:"password"`
		Email    string          `json:"email"`
		Type     string          `json:"type"`
		Role     models.UserRole `json:"role"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if err := validUsername(req.Username); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Password) < 8 {
		writeError(c, http.StatusBadRequest, "Password must be at least 8 characters")
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	if req.Email != "" {
		if _, parseErr := mail.ParseAddress(req.Email); parseErr != nil {
			writeError(c, http.StatusBadRequest, "Invalid email address")
			return
		}
	}
	if req.Role != models.RoleAdmin && req.Role != models.RoleViewer {
		req.Role = models.RoleViewer
	}

	if req.Type == "" {
		req.Type = "standard"
	}
	user, err := h.auth.CreateUser(c.Request.Context(), auth.CreateUserParams{
		Username: req.Username,
		Password: req.Password,
		Email:    req.Email,
		UserType: req.Type,
		Role:     req.Role,
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

	h.logAdminAction(c, &adminLogActionParams{Action: "create_user", Target: req.Username})

	writeSuccess(c, user)
}

// AdminGetUser returns a single user's details
func (h *Handler) AdminGetUser(c *gin.Context) {
	username := c.Param("username")

	user, err := h.auth.GetUser(c.Request.Context(), username)
	if err != nil {
		writeError(c, http.StatusNotFound, errUserNotFound)
		return
	}

	writeSuccess(c, user)
}

// AdminUpdateUser updates a user's details. Prevents demoting or disabling the last admin.
func (h *Handler) AdminUpdateUser(c *gin.Context) {
	username := c.Param("username")

	var req struct {
		Role        string                 `json:"role"`
		Enabled     *bool                  `json:"enabled"`
		Email       string                 `json:"email"`
		Permissions map[string]interface{} `json:"permissions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}
	if req.Role != "" && req.Role != string(models.RoleAdmin) && req.Role != string(models.RoleViewer) {
		req.Role = string(models.RoleViewer)
	}

	updates := map[string]interface{}{}
	if req.Role != "" {
		updates["role"] = req.Role
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.Email != "" {
		if _, parseErr := mail.ParseAddress(req.Email); parseErr != nil {
			writeError(c, http.StatusBadRequest, "Invalid email address")
			return
		}
		updates["email"] = req.Email
	}
	if req.Permissions != nil {
		updates["permissions"] = req.Permissions
	}

	if err := h.auth.UpdateUser(c.Request.Context(), username, updates); err != nil {
		if errors.Is(err, auth.ErrCannotDemoteLastAdmin) {
			writeError(c, http.StatusBadRequest, "Cannot demote or disable the last admin account")
			return
		}
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	h.logAdminAction(c, &adminLogActionParams{Action: "update_user", Target: username, Details: updates})

	user, err := h.auth.GetUser(c.Request.Context(), username)
	if err != nil {
		h.log.Error("Failed to fetch updated user %s: %v", username, err)
		writeSuccess(c, map[string]string{"message": "User updated"})
		return
	}
	writeSuccess(c, user)
}

// AdminDeleteUser deletes a user
func (h *Handler) AdminDeleteUser(c *gin.Context) {
	username := c.Param("username")

	// Prevent admin from deleting their own account
	if sess := getSession(c); sess != nil && sess.Username == username {
		writeError(c, http.StatusForbidden, "Cannot delete your own account")
		return
	}

	if err := h.auth.DeleteUser(c.Request.Context(), username); err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeError(c, http.StatusNotFound, "User not found")
			return
		}
		if errors.Is(err, auth.ErrCannotDemoteLastAdmin) {
			writeError(c, http.StatusBadRequest, "Cannot delete the last admin account")
			return
		}
		h.log.Error("Failed to delete user %s: %v", username, err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	h.logAdminAction(c, &adminLogActionParams{Action: "delete_user", Target: username})
	writeSuccess(c, nil)
}

// AdminChangePassword changes a user's password (admin action)
func (h *Handler) AdminChangePassword(c *gin.Context) {
	username := c.Param("username")

	var req struct {
		NewPassword string `json:"new_password"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	if req.NewPassword == "" {
		writeError(c, http.StatusBadRequest, "New password required")
		return
	}

	if len(req.NewPassword) < 8 {
		writeError(c, http.StatusBadRequest, "New password must be at least 8 characters")
		return
	}

	if err := h.auth.SetPassword(c.Request.Context(), username, req.NewPassword); err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	h.logAdminAction(c, &adminLogActionParams{Action: "change_password", Target: username})
	writeSuccess(c, map[string]string{"status": "password_changed"})
}

// AdminChangeOwnPassword lets an admin change the admin account password directly
func (h *Handler) AdminChangeOwnPassword(c *gin.Context) {
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

	if err := h.auth.ChangeAdminPassword(c.Request.Context(), req.CurrentPassword, req.NewPassword); err != nil {
		writeError(c, http.StatusUnauthorized, "Current password is incorrect")
		return
	}

	h.logAdminAction(c, &adminLogActionParams{Action: "change_admin_password"})
	writeSuccess(c, map[string]string{"status": "password_changed"})
}

// AdminBulkUsers performs a bulk action (delete, enable, disable) on multiple users.
func (h *Handler) AdminBulkUsers(c *gin.Context) {
	var req struct {
		Usernames []string `json:"usernames"`
		Action    string   `json:"action"`
	}
	if !BindJSON(c, &req, "") {
		return
	}
	if len(req.Usernames) == 0 {
		writeError(c, http.StatusBadRequest, "usernames must not be empty")
		return
	}
	if len(req.Usernames) > 200 {
		writeError(c, http.StatusBadRequest, "too many usernames (max 200)")
		return
	}
	if req.Action != "delete" && req.Action != "enable" && req.Action != "disable" {
		writeError(c, http.StatusBadRequest, `action must be "delete", "enable", or "disable"`)
		return
	}

	sess := getSession(c)
	currentUser := ""
	if sess != nil {
		currentUser = sess.Username
	}

	// Last-admin protection is enforced in auth (UpdateUser demote/disable; DeleteUser)
	// and returns ErrCannotDemoteLastAdmin. No redundant handler snapshot — it was racy.
	var successCount, failedCount int
	errs := make([]string, 0)

	for _, username := range req.Usernames {
		if username == "" || username == "admin" {
			continue
		}
		if username == currentUser && (req.Action == "delete" || req.Action == "disable") {
			failedCount++
			errs = append(errs, fmt.Sprintf("%s: cannot %s your own account", username, req.Action))
			continue
		}
		var opErr error
		switch req.Action {
		case "delete":
			opErr = h.auth.DeleteUser(c.Request.Context(), username)
			if opErr == nil {
				h.logAdminAction(c, &adminLogActionParams{Action: "bulk_delete_user", Target: username})
			}
		case "enable":
			opErr = h.auth.UpdateUser(c.Request.Context(), username, map[string]interface{}{"enabled": true})
			if opErr == nil {
				h.logAdminAction(c, &adminLogActionParams{Action: "bulk_enable_user", Target: username})
			}
		case "disable":
			opErr = h.auth.UpdateUser(c.Request.Context(), username, map[string]interface{}{"enabled": false})
			if opErr == nil {
				h.logAdminAction(c, &adminLogActionParams{Action: "bulk_disable_user", Target: username})
			}
		}
		if opErr != nil {
			h.log.Error("bulk %s user %s: %v", req.Action, username, opErr)
			failedCount++
			errs = append(errs, fmt.Sprintf("%s: %v", username, opErr))
		} else {
			successCount++
		}
	}

	writeSuccess(c, map[string]interface{}{
		"success": successCount,
		"failed":  failedCount,
		"errors":  errs,
	})
}
