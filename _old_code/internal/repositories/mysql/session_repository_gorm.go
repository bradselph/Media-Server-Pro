// Package mysql provides MySQL/GORM implementations of repository interfaces.
package mysql

import (
	"context"
	"time"

	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"

	"gorm.io/gorm"
)

// SessionRepositoryGORM implements repositories.SessionRepository using GORM
type SessionRepositoryGORM struct {
	db *gorm.DB
}

// NewSessionRepositoryGORM creates a new GORM-backed session repository
func NewSessionRepositoryGORM(db *gorm.DB) repositories.SessionRepository {
	return &SessionRepositoryGORM{db: db}
}

// Create inserts a new session
func (r *SessionRepositoryGORM) Create(ctx context.Context, session *models.Session) error {
	return r.db.WithContext(ctx).Create(session).Error
}

// Get retrieves a session by ID
func (r *SessionRepositoryGORM) Get(ctx context.Context, id string) (*models.Session, error) {
	var session models.Session
	err := r.db.WithContext(ctx).First(&session, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// Delete removes a session
func (r *SessionRepositoryGORM) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.Session{}, "id = ?", id).Error
}

// DeleteExpired removes all expired sessions
func (r *SessionRepositoryGORM) DeleteExpired(ctx context.Context) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Where("expires_at < ?", now).
		Delete(&models.Session{}).Error
}

// List retrieves all sessions
func (r *SessionRepositoryGORM) List(ctx context.Context) ([]*models.Session, error) {
	var sessions []*models.Session
	err := r.db.WithContext(ctx).Find(&sessions).Error
	return sessions, err
}

// ListByUser retrieves all sessions for a specific user
func (r *SessionRepositoryGORM) ListByUser(ctx context.Context, userID string) ([]*models.Session, error) {
	var sessions []*models.Session
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Find(&sessions).Error
	return sessions, err
}
