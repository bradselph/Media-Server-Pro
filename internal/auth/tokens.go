package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"
)

// CreateAPIToken generates a new API token for the user and persists it.
// If ttl > 0 the token will expire after that duration; ttl=0 means no expiry.
// Returns the raw token value (shown only once) and the stored record.
func (m *Module) CreateAPIToken(ctx context.Context, userID, name string, ttl time.Duration) (rawToken string, rec *repositories.APITokenRecord, err error) {
	rawToken = generateSessionID() // 32 bytes → URL-safe base64 (~44 chars)
	hash := hashToken(rawToken)
	rec = &repositories.APITokenRecord{
		ID:        generateID(),
		UserID:    userID,
		Name:      name,
		TokenHash: hash,
		CreatedAt: time.Now(),
	}
	if ttl > 0 {
		rec.ExpiresAt = new(time.Now().Add(ttl))
	}
	if err = m.tokenRepo.Create(ctx, rec); err != nil {
		return "", nil, fmt.Errorf("create api token: %w", err)
	}
	return rawToken, rec, nil
}

// ListAPITokens returns all tokens for the given user (without the raw value).
func (m *Module) ListAPITokens(ctx context.Context, userID string) ([]*repositories.APITokenRecord, error) {
	return m.tokenRepo.ListByUser(ctx, userID)
}

// DeleteAPIToken revokes a token by ID, scoped to the owning user.
func (m *Module) DeleteAPIToken(ctx context.Context, tokenID, userID string) error {
	return m.tokenRepo.Delete(ctx, tokenID, userID)
}

// ValidateAPIToken looks up a raw bearer token and, if valid, returns the user
// and a synthetic Session that can be stored in the gin context like a real session.
func (m *Module) ValidateAPIToken(ctx context.Context, rawToken string) (*models.Session, *models.User, error) {
	hash := hashToken(rawToken)
	rec, err := m.tokenRepo.GetByHash(ctx, hash)
	if err != nil {
		return nil, nil, fmt.Errorf("validate api token: %w", err)
	}
	if rec == nil {
		return nil, nil, ErrInvalidCredentials
	}
	if rec.ExpiresAt != nil && time.Now().After(*rec.ExpiresAt) {
		// Clean up expired token so it doesn't accumulate in the database.
		go func() { //nolint:gosec // G118: background context intentional for async cleanup goroutine
			if delErr := m.tokenRepo.Delete(context.Background(), rec.ID, rec.UserID); delErr != nil {
				m.log.Warn("Failed to delete expired API token %s: %v", rec.ID, delErr)
			}
		}()
		return nil, nil, ErrSessionExpired
	}

	user, err := m.userRepo.GetByID(ctx, rec.UserID)
	if err != nil {
		return nil, nil, fmt.Errorf("load token owner: %w", err)
	}
	if user == nil || !user.Enabled {
		return nil, nil, ErrAccountDisabled
	}

	// Update last_used_at asynchronously — don't block the request on a non-critical write.
	go func() { //nolint:gosec // G118: background context intentional for async non-critical write
		if err := m.tokenRepo.UpdateLastUsed(context.Background(), hash); err != nil {
			m.log.Warn("Failed to update API token last_used_at: %v", err)
		}
	}()

	// Synthetic session — not stored in the sessions table; used only for context propagation.
	// ExpiresAt is bounded by the token's actual TTL when set, otherwise defaults to 24h.
	expiresAt := time.Now().Add(24 * time.Hour)
	if rec.ExpiresAt != nil {
		expiresAt = *rec.ExpiresAt
	}
	session := &models.Session{
		ID:           "token:" + rec.ID,
		UserID:       user.ID,
		Username:     user.Username,
		Role:         user.Role,
		CreatedAt:    rec.CreatedAt,
		ExpiresAt:    expiresAt,
		LastActivity: time.Now(),
	}
	return session, user, nil
}

// hashToken returns the SHA-256 hex digest of the raw token.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
