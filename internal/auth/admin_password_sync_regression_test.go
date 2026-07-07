package auth

import (
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
)

// TestSyncAdminConfigPassword guards the fix for the built-in-admin credential
// desync: changing the admin password through the generic user paths
// (UpdatePassword / SetPassword) must update cfg.Admin.PasswordHash — the only
// credential AdminAuthenticate checks — with an UNSALTED hash of the new
// password, so the new password works at login and the old one stops working.
func TestSyncAdminConfigPassword(t *testing.T) {
	mgr := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	oldHash, err := bcrypt.GenerateFromPassword([]byte("oldpassword"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash old password: %v", err)
	}
	if err := mgr.Update(func(c *config.Config) {
		c.Admin.Enabled = true
		c.Admin.Username = "admin"
		c.Admin.PasswordHash = string(oldHash)
	}); err != nil {
		t.Fatalf("setup config update: %v", err)
	}

	m := &Module{config: mgr, log: logger.New("test")}

	// Non-admin username is a no-op: cfg.Admin.PasswordHash must be untouched.
	if err := m.syncAdminConfigPasswordIfNeeded("alice", "somethingelse"); err != nil {
		t.Fatalf("non-admin sync should be a no-op, got: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(mgr.Get().Admin.PasswordHash), []byte("oldpassword")) != nil {
		t.Fatal("non-admin sync must not modify cfg.Admin.PasswordHash")
	}

	// Admin username: cfg hash is replaced with a login-usable hash of the new pw.
	if err := m.syncAdminConfigPasswordIfNeeded("admin", "newpassword"); err != nil {
		t.Fatalf("admin sync failed: %v", err)
	}
	got := mgr.Get().Admin.PasswordHash

	// AdminAuthenticate does bcrypt.CompareHashAndPassword(cfg.Admin.PasswordHash,
	// password) with NO salt — the new password must validate that way.
	if err := bcrypt.CompareHashAndPassword([]byte(got), []byte("newpassword")); err != nil {
		t.Fatalf("admin login would reject the new password after sync: %v", err)
	}
	// And the old password must no longer validate.
	if bcrypt.CompareHashAndPassword([]byte(got), []byte("oldpassword")) == nil {
		t.Fatal("old admin password must stop working after a password change")
	}
}
