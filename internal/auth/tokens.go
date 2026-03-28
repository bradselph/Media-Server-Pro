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
// Returns the raw token value (shown only once) and the stored record.
func (m *Module) CreateAPIToken(ctx context.Context, userID, name string) (rawToken string, rec *repositories.APITokenRecord, err error) {
	rawToken = generateSessionID() // 32 bytes → URL-safe base64 (~44 chars)
	hash := hashToken(rawToken)
	rec = &repositories.APITokenRecord{
		ID:        generateID(),
		UserID:    userID,
		Name:      name,
		TokenHash: hash,
		CreatedAt: time.Now(),
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

	user, err := m.userRepo.GetByID(ctx, rec.UserID)
	if err != nil {
		return nil, nil, fmt.Errorf("load token owner: %w", err)
	}
	if user == nil || !user.Enabled {
		return nil, nil, ErrAccountDisabled
	}

	// Update last_used_at asynchronously — don't block the request on a non-critical write.
	go func() {
		_ = m.tokenRepo.UpdateLastUsed(context.Background(), hash)
	}()

	// Synthetic session — not stored in the sessions table; used only for context propagation.
	session := &models.Session{
		ID:           "token:" + rec.ID,
		UserID:       user.ID,
		Username:     user.Username,
		Role:         user.Role,
		CreatedAt:    rec.CreatedAt,
		ExpiresAt:    time.Now().Add(365 * 24 * time.Hour), // tokens don't expire
		LastActivity: time.Now(),
	}
	return session, user, nil
}

// hashToken returns the SHA-256 hex digest of the raw token.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
