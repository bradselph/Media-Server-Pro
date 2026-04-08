package models

import "time"

// DataDeletionRequestStatus represents the lifecycle of a deletion request.
type DataDeletionRequestStatus string

const (
	DeletionRequestPending  DataDeletionRequestStatus = "pending"
	DeletionRequestApproved DataDeletionRequestStatus = "approved"
	DeletionRequestDenied   DataDeletionRequestStatus = "denied"
)

// DataDeletionRequest is a user-submitted request to have their data deleted.
// Admins review and decide whether to approve (which triggers actual deletion)
// or deny the request. AdminNotes is excluded from JSON to prevent leaking
// admin-internal notes if this model is ever returned to non-admin callers.
// Use AdminDeletionRequestView for admin responses that include notes.
type DataDeletionRequest struct {
	ID         string                    `json:"id"`
	UserID     string                    `json:"user_id"`
	Username   string                    `json:"username"`
	Email      string                    `json:"email,omitempty"`
	Reason     string                    `json:"reason,omitempty"`
	Status     DataDeletionRequestStatus `json:"status"`
	CreatedAt  time.Time                 `json:"created_at"`
	ReviewedAt *time.Time                `json:"reviewed_at,omitempty"`
	ReviewedBy string                    `json:"reviewed_by,omitempty"`
	AdminNotes string                    `json:"-"`
}

// AdminDeletionRequestView extends DataDeletionRequest with AdminNotes visible
// in JSON. Used only by admin-facing endpoints.
type AdminDeletionRequestView struct {
	DataDeletionRequest
	AdminNotes string `json:"admin_notes,omitempty"`
}
