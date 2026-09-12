package database

import (
	"database/sql"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"media-server-pro/pkg/models"
)

// deadMySQLListener starts a TCP listener that accepts connections and closes
// them immediately, counting each accept. A MySQL ping against it always fails
// (no handshake), and the counter records whether a ping was actually attempted.
func deadMySQLListener(t *testing.T) (addr string, accepts *atomic.Int64) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	accepts = &atomic.Int64{}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			accepts.Add(1)
			_ = conn.Close()
		}
	}()
	return ln.Addr().String(), accepts
}

// poolTo builds a pool aimed at addr. sql.Open is lazy, so this never blocks.
func poolTo(t *testing.T, addr string) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", "u:p@tcp("+addr+")/testdb?timeout=2s")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// Health() used to gate its liveness ping on the cached healthy flag
// (`if healthy && sqlDB != nil`). That latched the module unhealthy for the rest
// of the process: the first failed ping cleared the flag, which then skipped the
// only check that could set it back, so one transient blip made
// /admin/database/status report "disconnected" and AdminExecuteQuery refuse to
// run until a restart. Health() must probe whenever a pool exists.
func TestHealth_PingsEvenWhenAlreadyUnhealthy(t *testing.T) {
	addr, accepts := deadMySQLListener(t)

	m := NewModule(nil)
	m.sqlDB = poolTo(t, addr)
	// The latched state: a previous ping already marked the module unhealthy.
	m.setHealth(false, "Ping failed: earlier blip")

	before := accepts.Load()
	status := m.Health()
	after := accepts.Load()

	if after <= before {
		t.Errorf("Health() attempted %d connections while unhealthy; want at least 1 — "+
			"a module that stops probing after its first failure can never report recovery", after-before)
	}
	if status.Status != models.StatusUnhealthy {
		t.Errorf("Health().Status = %v, want %v (the pool is unreachable)", status.Status, models.StatusUnhealthy)
	}
}

// A nil pool means Stop() has run. That is not a state a ping should clear, and
// Health() must not dial anything.
func TestHealth_NilPoolReportsStoppedWithoutPinging(t *testing.T) {
	m := NewModule(nil)
	m.setHealth(false, "Stopped")

	status := m.Health()
	if status.Status != models.StatusUnhealthy {
		t.Errorf("Health().Status = %v, want %v", status.Status, models.StatusUnhealthy)
	}
	if status.Message != "Stopped" {
		t.Errorf("Health().Message = %q, want %q — a nil pool must not have its message overwritten", status.Message, "Stopped")
	}
}

// A failed probe must also be written back to the cached flags, so IsConnected()
// (used by module Start gates) agrees with what Health() just observed.
func TestHealth_FailedPingUpdatesIsConnected(t *testing.T) {
	addr, _ := deadMySQLListener(t)

	m := NewModule(nil)
	m.sqlDB = poolTo(t, addr)
	m.setHealth(true, "Connected")

	if !m.IsConnected() {
		t.Fatal("precondition: IsConnected() should be true before the probe")
	}
	if status := m.Health(); status.Status != models.StatusUnhealthy {
		t.Fatalf("Health().Status = %v, want %v", status.Status, models.StatusUnhealthy)
	}
	if m.IsConnected() {
		t.Error("IsConnected() still true after Health() observed a failed ping")
	}
}

func TestHealth_ReportsModuleNameAndTimestamp(t *testing.T) {
	m := NewModule(nil)
	start := time.Now()

	status := m.Health()
	if status.Name != "database" {
		t.Errorf("Health().Name = %q, want %q", status.Name, "database")
	}
	if status.CheckedAt.Before(start.Add(-time.Second)) {
		t.Errorf("Health().CheckedAt = %v, want a timestamp at or after %v", status.CheckedAt, start)
	}
}
