package auth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/pkg/models"
)

// TestCreateSession_ClampsNonPositiveTimeout is a regression test for the
// still-live half of the AUTH_SESSION_TIMEOUT_HOURS=0 bug: the env-ingestion
// guard only runs on first boot, so a persisted config.json (or any path that
// leaves auth.session_timeout <= 0) reached createSession, where
// ExpiresAt = now + 0 made every non-admin session expire the instant it was
// created — silently breaking all logins. The point-of-use clamp must fall back
// to a sane default. Uses a mock session repo, so it runs without a database.
func TestCreateSession_ClampsNonPositiveTimeout(t *testing.T) {
	cfgMgr := config.NewManager(filepath.Join(t.TempDir(), "cfg.json"))
	if err := cfgMgr.Update(func(c *config.Config) { c.Auth.SessionTimeout = 0 }); err != nil {
		t.Fatalf("Update (session_timeout=0 must be accepted, not rejected): %v", err)
	}

	m := &Module{
		config:        cfgMgr,
		sessionRepo:   fnd0012SessionRepo{},
		sessions:      make(map[string]*models.Session),
		adminSessions: make(map[string]*models.AdminSession),
	}
	user := &models.User{ID: "u1", Username: "alice", Role: models.RoleViewer}

	sess, err := m.createSession(context.Background(), user, &sessionRequestContext{})
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}
	if ttl := time.Until(sess.ExpiresAt); ttl < 23*time.Hour {
		t.Fatalf("session created with auth.session_timeout=0 expires in %v; expected the ~24h clamp (otherwise every login breaks)", ttl)
	}
}
