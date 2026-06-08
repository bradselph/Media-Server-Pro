package auth

import (
	"context"
	"sync"
	"testing"

	"media-server-pro/pkg/models"
)

// fnd0012SessionRepo is a no-op SessionRepository stub: removeExpiredSession only
// calls Delete (and logs solely on a Delete error), so every method can return
// the zero value without exercising any real storage.
type fnd0012SessionRepo struct{}

func (fnd0012SessionRepo) Create(context.Context, *models.Session) error { return nil }
func (fnd0012SessionRepo) Get(context.Context, string) (*models.Session, error) {
	return nil, ErrSessionNotFound
}
func (fnd0012SessionRepo) Update(context.Context, *models.Session) error   { return nil }
func (fnd0012SessionRepo) Delete(context.Context, string) error            { return nil }
func (fnd0012SessionRepo) DeleteExpired(context.Context) error             { return nil }
func (fnd0012SessionRepo) List(context.Context) ([]*models.Session, error) { return nil, nil }
func (fnd0012SessionRepo) ListByUser(context.Context, string) ([]*models.Session, error) {
	return nil, nil
}

// TestFND0012_RemoveExpiredSession_ClearsAdminMap is a regression test for the
// bug where removeExpiredSession deleted only from m.sessions, leaking expired
// admin sessions in m.adminSessions (where getOrLoadSession routes admin roles)
// until the periodic cleanup tick. It must evict from BOTH maps.
func TestFND0012_RemoveExpiredSession_ClearsAdminMap(t *testing.T) {
	m := &Module{
		sessions:      make(map[string]*models.Session),
		adminSessions: make(map[string]*models.AdminSession),
		sessionsMu:    sync.RWMutex{},
		sessionRepo:   fnd0012SessionRepo{},
	}
	m.adminSessions["sess-admin"] = &models.AdminSession{
		Session: models.Session{ID: "sess-admin", Username: "root", Role: models.RoleAdmin},
	}

	m.removeExpiredSession(context.Background(), "sess-admin")

	m.sessionsMu.RLock()
	_, stillThere := m.adminSessions["sess-admin"]
	m.sessionsMu.RUnlock()
	if stillThere {
		t.Error("expired admin session leaked in m.adminSessions after removeExpiredSession")
	}
}
