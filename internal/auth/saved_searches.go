package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"media-server-pro/internal/repositories"
)

var errInvalidSavedSearchParams = errors.New("userID and name are required")

const (
	maxSavedSearchesPerUser = 25
	maxSavedSearchNameLen   = 80
	maxSavedSearchQueryLen  = 200
)

// SaveSearch persists a search definition for a user so the homepage row
// can later surface new matches. Imposes a per-user cap so a single user
// can't grow this table unbounded.
func (m *Module) SaveSearch(ctx context.Context, rec *repositories.SavedSearchRecord) (*repositories.SavedSearchRecord, error) {
	if rec == nil || rec.UserID == "" {
		return nil, errInvalidSavedSearchParams
	}
	rec.Name = strings.TrimSpace(rec.Name)
	if rec.Name == "" {
		return nil, errInvalidSavedSearchParams
	}
	if len(rec.Name) > maxSavedSearchNameLen {
		rec.Name = rec.Name[:maxSavedSearchNameLen]
	}
	if len(rec.Query) > maxSavedSearchQueryLen {
		rec.Query = rec.Query[:maxSavedSearchQueryLen]
	}
	if rec.TagMode != "and" {
		rec.TagMode = "or"
	}

	existing, err := m.savedSearchRepo.List(ctx, rec.UserID)
	if err != nil {
		return nil, fmt.Errorf("list existing saved searches: %w", err)
	}
	if len(existing) >= maxSavedSearchesPerUser {
		return nil, fmt.Errorf("saved-search limit reached (%d)", maxSavedSearchesPerUser)
	}

	rec.ID = generateID()
	rec.CreatedAt = time.Now()
	rec.LastSeenAt = rec.CreatedAt
	if err := m.savedSearchRepo.Create(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// ListSavedSearches returns all saved searches for the given user, sorted
// newest first by the repository.
func (m *Module) ListSavedSearches(ctx context.Context, userID string) ([]*repositories.SavedSearchRecord, error) {
	if userID == "" {
		return nil, errInvalidSavedSearchParams
	}
	return m.savedSearchRepo.List(ctx, userID)
}

// GetSavedSearch returns a single saved-search by id, scoped to the user
// so one user can't read another's saved searches by guessing an id.
func (m *Module) GetSavedSearch(ctx context.Context, id, userID string) (*repositories.SavedSearchRecord, error) {
	if id == "" || userID == "" {
		return nil, errInvalidSavedSearchParams
	}
	return m.savedSearchRepo.Get(ctx, id, userID)
}

// DeleteSavedSearch removes a saved search owned by userID. No error is
// returned when the row is missing — DELETE is idempotent here.
func (m *Module) DeleteSavedSearch(ctx context.Context, id, userID string) error {
	if id == "" || userID == "" {
		return errInvalidSavedSearchParams
	}
	return m.savedSearchRepo.Delete(ctx, id, userID)
}

// TouchSavedSearch updates the last_seen_at timestamp on a saved search
// so the "new since" diff resets after the user reviews matches.
func (m *Module) TouchSavedSearch(ctx context.Context, id, userID string) error {
	if id == "" || userID == "" {
		return errInvalidSavedSearchParams
	}
	return m.savedSearchRepo.UpdateLastSeen(ctx, id, userID, time.Now())
}
