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
	err := r.db.WithContext(ctx).First(&session, sqlIDEq, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, repositories.ErrSessionNotFound
		}
		return nil, err
	}
	return &session, nil
}

// Update persists all updatable session fields (LastActivity, Username, Role, ExpiresAt, IPAddress, UserAgent)
// so that role/username/expiry changes are reflected and callers do not lose updates.
func (r *SessionRepository) Update(ctx context.Context, session *models.Session) error {
	result := r.db.WithContext(ctx).Model(session).Where(sqlIDEq, session.ID).Updates(map[string]interface{}{
		"last_activity": session.LastActivity,
		"username":      session.Username,
		"role":          session.Role,
		"expires_at":    session.ExpiresAt,
		"ip_address":    session.IPAddress,
		"user_agent":    session.UserAgent,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return repositories.ErrSessionNotFound
	}
	return nil
}

func (r *SessionRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Delete(&models.Session{}, sqlIDEq, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return repositories.ErrSessionNotFound
	}
	return nil
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
