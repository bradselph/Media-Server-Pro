// Package mysql provides MySQL/GORM implementations of repository interfaces.
package mysql

import (
	"context"
	"errors"
	"time"

	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"

	"gorm.io/gorm"
)

// SessionRepository implements repositories.SessionRepository using GORM
type SessionRepository struct {
	db *gorm.DB
}

// NewSessionRepository creates a new GORM-backed session repository
func NewSessionRepository(db *gorm.DB) repositories.SessionRepository {
	return &SessionRepository{db: db}
}

// Create inserts a new session
func (r *SessionRepository) Create(ctx context.Context, session *models.Session) error {
	return r.db.WithContext(ctx).Create(session).Error
}

// Get retrieves a session by ID
func (r *SessionRepository) Get(ctx context.Context, id string) (*models.Session, error) {
	var session models.Session
	err := r.db.WithContext(ctx).First(&session, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, repositories.ErrSessionNotFound
		}
		return nil, err
	}
	return &session, nil
}

// TODO: Silent failure — Delete does not check RowsAffected. If the session ID does
// not exist, the operation succeeds silently. This is inconsistent with the Get method
// which returns repositories.ErrSessionNotFound. Consider checking RowsAffected and
// returning ErrSessionNotFound when no rows are deleted.
func (r *SessionRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.Session{}, "id = ?", id).Error
}

// DeleteExpired removes all expired sessions
func (r *SessionRepository) DeleteExpired(ctx context.Context) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Where("expires_at < ?", now).
		Delete(&models.Session{}).Error
}

// List retrieves all sessions
func (r *SessionRepository) List(ctx context.Context) ([]*models.Session, error) {
	var sessions []*models.Session
	err := r.db.WithContext(ctx).Find(&sessions).Error
	return sessions, err
}

// ListByUser retrieves all sessions for a specific user
func (r *SessionRepository) ListByUser(ctx context.Context, userID string) ([]*models.Session, error) {
	var sessions []*models.Session
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Find(&sessions).Error
	return sessions, err
}
