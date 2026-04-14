package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/auth"
	"media-server-pro/internal/repositories"
	repoMysql "media-server-pro/internal/repositories/mysql"
	"media-server-pro/pkg/models"
)

const errDeletionRequestsUnavailable = "Data deletion request service unavailable"

// requireDeletionRepo ensures the deletion-request repository is ready.
// Lazy-initialises on first call so that GORM() is not captured before the
// database module's Start() runs (which would leave r.db nil and cause a panic).
func (h *Handler) requireDeletionRepo(c *gin.Context) bool {
	if h.deletionRequests == nil {
		h.deletionRequestsMu.Lock()
		if h.deletionRequests == nil {
			db := h.database.GORM()
			if db == nil {
				h.deletionRequestsMu.Unlock()
				writeError(c, http.StatusServiceUnavailable, errDeletionRequestsUnavailable)
				return false
			}
			h.deletionRequests = repoMysql.NewDataDeletionRequestRepository(db)
		}
		h.deletionRequestsMu.Unlock()
	}
	return true
}

// RequestDataDeletion allows an authenticated user to submit a request to have their data deleted.
// The request is queued for admin review — no immediate deletion occurs.
func (h *Handler) RequestDataDeletion(c *gin.Context) {
	if !h.requireDeletionRepo(c) {
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

	ctx := c.Request.Context()

	// Check for an existing pending request from this user.
	count, err := h.deletionRequests.CountPendingByUser(ctx, user.ID)
	if err != nil {
		h.log.Error("Failed to check existing deletion requests for %s: %v", user.Username, err)
		writeError(c, http.StatusInternalServerError, errDeletionRequestsUnavailable)
		return
	}
	if count > 0 {
		writeError(c, http.StatusConflict, "You already have a pending data deletion request. Please wait for admin review.")
		return
	}

	id := generateRandomString(32)
	record := &repositories.DataDeletionRequestRecord{
		ID:       id,
		UserID:   user.ID,
		Username: user.Username,
		Email:    user.Email,
		Reason:   req.Reason,
		Status:   string(models.DeletionRequestPending),
	}
	if err := h.deletionRequests.Create(ctx, record); err != nil {
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

// AdminListDeletionRequests returns all data deletion requests (admin only).
func (h *Handler) AdminListDeletionRequests(c *gin.Context) {
	if !h.requireDeletionRepo(c) {
		return
	}
	status := c.Query("status")
	records, err := h.deletionRequests.ListByStatus(c.Request.Context(), status)
	if err != nil {
		h.log.Error("Failed to list data deletion requests: %v", err)
		writeError(c, http.StatusInternalServerError, errDeletionRequestsUnavailable)
		return
	}
	reqs := make([]*models.AdminDeletionRequestView, len(records))
	for i, r := range records {
		reqs[i] = &models.AdminDeletionRequestView{
			DataDeletionRequest: models.DataDeletionRequest{
				ID:         r.ID,
				UserID:     r.UserID,
				Username:   r.Username,
				Email:      r.Email,
				Reason:     r.Reason,
				Status:     models.DataDeletionRequestStatus(r.Status),
				CreatedAt:  r.CreatedAt,
				ReviewedAt: r.ReviewedAt,
				ReviewedBy: r.ReviewedBy,
			},
			AdminNotes: r.AdminNotes,
		}
	}
	writeSuccess(c, reqs)
}

// AdminProcessDeletionRequest approves or denies a data deletion request (admin only).
// On approve, the user account is permanently deleted. On deny, the request is closed with a note.
func (h *Handler) AdminProcessDeletionRequest(c *gin.Context) {
	if !h.requireDeletionRepo(c) {
		return
	}
	requestID, ok := RequireParamID(c, "id")
	if !ok {
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

	// Fetch the request.
	dr, err := h.deletionRequests.Get(ctx, requestID)
	if err != nil || dr == nil {
		writeError(c, http.StatusNotFound, "Data deletion request not found")
		return
	}
	if dr.Status != string(models.DeletionRequestPending) {
		writeError(c, http.StatusConflict, "Request has already been processed")
		return
	}

	newStatus := string(models.DeletionRequestDenied)
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
		newStatus = string(models.DeletionRequestApproved)
	}

	if err := h.deletionRequests.UpdateStatus(ctx, requestID, newStatus, adminSession.Username, req.AdminNotes); err != nil {
		if req.Action == "approve" {
			// User was already deleted but status persistence failed — surface the inconsistency.
			h.log.Error("Failed to mark deletion request %s approved after user deletion: %v", requestID, err)
			writeError(c, http.StatusInternalServerError, "User deleted but status update failed — check logs")
			return
		}
		h.log.Error("Failed to update deletion request %s: %v", requestID, err)
		writeError(c, http.StatusInternalServerError, errDeletionRequestsUnavailable)
		return
	}

	h.log.Info("Admin %s %s data deletion request %s for user %s", adminSession.Username, req.Action+"d", requestID, dr.Username)
	writeSuccess(c, map[string]string{"status": newStatus})
}
