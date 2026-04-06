package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/auth"
	"media-server-pro/pkg/models"
)

const errDeletionRequestsUnavailable = "Data deletion request service unavailable"

func (h *Handler) requireDeletionDB(c *gin.Context) bool {
	if h.database == nil || h.database.DB() == nil {
		writeError(c, http.StatusServiceUnavailable, errDeletionRequestsUnavailable)
		return false
	}
	return true
}

// RequestDataDeletion allows an authenticated user to submit a request to have their data deleted.
// The request is queued for admin review — no immediate deletion occurs.
func (h *Handler) RequestDataDeletion(c *gin.Context) {
	if !h.requireDeletionDB(c) {
		return
	}
	session := RequireSession(c)
	if session == nil {
		return
	}
	user := getUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	// Check for an existing pending request from this user.
	db := h.database.DB()
	ctx := c.Request.Context()
	var existing int
	row := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM data_deletion_requests WHERE user_id = ? AND status = 'pending'`,
		user.ID,
	)
	if err := row.Scan(&existing); err != nil {
		h.log.Error("Failed to check existing deletion requests for %s: %v", user.Username, err)
		writeError(c, http.StatusInternalServerError, errDeletionRequestsUnavailable)
		return
	}
	if existing > 0 {
		writeError(c, http.StatusConflict, "You already have a pending data deletion request. Please wait for admin review.")
		return
	}

	id := generateRandomString(32)
	_, err := db.ExecContext(ctx,
		`INSERT INTO data_deletion_requests (id, user_id, username, email, reason, status, created_at)
		 VALUES (?, ?, ?, ?, ?, 'pending', ?)`,
		id, user.ID, user.Username, user.Email, req.Reason, time.Now().UTC(),
	)
	if err != nil {
		h.log.Error("Failed to create data deletion request for %s: %v", user.Username, err)
		writeError(c, http.StatusInternalServerError, errDeletionRequestsUnavailable)
		return
	}

	h.log.Info("Data deletion request created by user %s (id: %s)", user.Username, id)
	writeSuccess(c, map[string]string{
		"status":  "submitted",
		"message": "Your data deletion request has been submitted and will be reviewed by an administrator.",
		"id":      id,
	})
}

// listDeletionRequests returns all data deletion requests, optionally filtered by status.
func (h *Handler) listDeletionRequests(ctx context.Context, status string) ([]*models.DataDeletionRequest, error) {
	db := h.database.DB()

	query := `SELECT id, user_id, username, COALESCE(email,''), COALESCE(reason,''), status,
	                 created_at, reviewed_at, COALESCE(reviewed_by,''), COALESCE(admin_notes,'')
	          FROM data_deletion_requests`
	args := []interface{}{}
	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC"

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*models.DataDeletionRequest
	for rows.Next() {
		r := &models.DataDeletionRequest{}
		var reviewedAt *time.Time
		if err := rows.Scan(
			&r.ID, &r.UserID, &r.Username, &r.Email, &r.Reason, &r.Status,
			&r.CreatedAt, &reviewedAt, &r.ReviewedBy, &r.AdminNotes,
		); err != nil {
			return nil, err
		}
		r.ReviewedAt = reviewedAt
		results = append(results, r)
	}
	return results, rows.Err()
}

// AdminListDeletionRequests returns all data deletion requests (admin only).
func (h *Handler) AdminListDeletionRequests(c *gin.Context) {
	if !h.requireDeletionDB(c) {
		return
	}
	status := c.Query("status")
	reqs, err := h.listDeletionRequests(c.Request.Context(), status)
	if err != nil {
		h.log.Error("Failed to list data deletion requests: %v", err)
		writeError(c, http.StatusInternalServerError, errDeletionRequestsUnavailable)
		return
	}
	if reqs == nil {
		reqs = []*models.DataDeletionRequest{}
	}
	writeSuccess(c, reqs)
}

// AdminProcessDeletionRequest approves or denies a data deletion request (admin only).
// On approve, the user account is permanently deleted. On deny, the request is closed with a note.
func (h *Handler) AdminProcessDeletionRequest(c *gin.Context) {
	if !h.requireDeletionDB(c) {
		return
	}
	requestID := c.Param("id")
	if requestID == "" {
		writeError(c, http.StatusBadRequest, "request id is required")
		return
	}

	var req struct {
		Action     string `json:"action"`      // "approve" or "deny"
		AdminNotes string `json:"admin_notes"` // optional
	}
	if !BindJSON(c, &req, "") {
		return
	}
	if req.Action != "approve" && req.Action != "deny" {
		writeError(c, http.StatusBadRequest, `action must be "approve" or "deny"`)
		return
	}

	adminSession := getSession(c)
	if adminSession == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	ctx := c.Request.Context()
	db := h.database.DB()

	// Fetch the request.
	var dr models.DataDeletionRequest
	var drStatus string
	row := db.QueryRowContext(ctx,
		`SELECT id, user_id, username, status FROM data_deletion_requests WHERE id = ?`,
		requestID,
	)
	if err := row.Scan(&dr.ID, &dr.UserID, &dr.Username, &drStatus); err != nil {
		writeError(c, http.StatusNotFound, "Data deletion request not found")
		return
	}
	dr.Status = models.DataDeletionRequestStatus(drStatus)
	if dr.Status != models.DeletionRequestPending {
		writeError(c, http.StatusConflict, "Request has already been processed")
		return
	}

	now := time.Now().UTC()

	if req.Action == "approve" {
		// Delete the user first. Only record the approval in the DB if deletion succeeds,
		// so the request doesn't get stuck as "approved" while the account still exists.
		if err := h.auth.DeleteUser(ctx, dr.Username); err != nil {
			if errors.Is(err, auth.ErrCannotDemoteLastAdmin) {
				writeError(c, http.StatusBadRequest, "Cannot delete the last admin account")
				return
			}
			h.log.Error("Failed to delete user %s for deletion request %s: %v", dr.Username, requestID, err)
			writeError(c, http.StatusInternalServerError, "User deletion failed — check logs")
			return
		}
		if _, err := db.ExecContext(ctx,
			`UPDATE data_deletion_requests SET status=?, reviewed_at=?, reviewed_by=?, admin_notes=? WHERE id=?`,
			string(models.DeletionRequestApproved), now, adminSession.Username, req.AdminNotes, requestID,
		); err != nil {
			// User was already deleted; log the DB failure but don't fail the request.
			h.log.Error("Failed to mark deletion request %s approved after user deletion: %v", requestID, err)
		}
		h.log.Info("Admin %s approved data deletion request %s — user %s deleted", adminSession.Username, requestID, dr.Username)
		writeSuccess(c, map[string]string{"status": string(models.DeletionRequestApproved)})
	} else {
		if _, err := db.ExecContext(ctx,
			`UPDATE data_deletion_requests SET status=?, reviewed_at=?, reviewed_by=?, admin_notes=? WHERE id=?`,
			string(models.DeletionRequestDenied), now, adminSession.Username, req.AdminNotes, requestID,
		); err != nil {
			h.log.Error("Failed to update deletion request %s: %v", requestID, err)
			writeError(c, http.StatusInternalServerError, errDeletionRequestsUnavailable)
			return
		}
		h.log.Info("Admin %s denied data deletion request %s for user %s", adminSession.Username, requestID, dr.Username)
		writeSuccess(c, map[string]string{"status": string(models.DeletionRequestDenied)})
	}
}
