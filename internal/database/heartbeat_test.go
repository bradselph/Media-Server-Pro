package database

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"media-server-pro/internal/config"
)

// heartbeatCfg is a recovery-enabled config with the guards set to values the
// tests can reason about.
func heartbeatCfg() config.DatabaseConfig {
	return config.DatabaseConfig{
		HeartbeatEnabled:    true,
		HeartbeatInterval:   30 * time.Second,
		HeartbeatThreshold:  5,
		RecoveryEnabled:     true,
		RecoveryCooldown:    15 * time.Minute,
		RecoveryMaxAttempts: 3,
	}
}

func TestRecoveryGate_FirstAttemptRuns(t *testing.T) {
	got := recoveryGate(heartbeatCfg(), 0, time.Time{}, time.Now())
	if got != recoveryRun {
		t.Errorf("first attempt with no prior recovery = %v, want recoveryRun", got)
	}
}

func TestRecoveryGate_DisabledNeverRuns(t *testing.T) {
	cfg := heartbeatCfg()
	cfg.RecoveryEnabled = false
	if got := recoveryGate(cfg, 0, time.Time{}, time.Now()); got != recoveryDisabled {
		t.Errorf("recovery disabled = %v, want recoveryDisabled", got)
	}
}

func TestRecoveryGate_CooldownBlocksRepeatAttempt(t *testing.T) {
	cfg := heartbeatCfg()
	now := time.Now()
	// One minute after an attempt, well inside the 15m cooldown.
	if got := recoveryGate(cfg, 1, now.Add(-1*time.Minute), now); got != recoveryCoolingDown {
		t.Errorf("1m after an attempt (cooldown %v) = %v, want recoveryCoolingDown", cfg.RecoveryCooldown, got)
	}
}

func TestRecoveryGate_RunsAfterCooldownElapses(t *testing.T) {
	cfg := heartbeatCfg()
	now := time.Now()
	if got := recoveryGate(cfg, 1, now.Add(-16*time.Minute), now); got != recoveryRun {
		t.Errorf("16m after an attempt (cooldown %v) = %v, want recoveryRun", cfg.RecoveryCooldown, got)
	}
}

// The cooldown must not be able to trap the gate forever: once attempts reach
// the cap, the answer is "exhausted" regardless of how long ago the last one was.
func TestRecoveryGate_ExhaustedAttemptsBlockEvenAfterCooldown(t *testing.T) {
	cfg := heartbeatCfg()
	now := time.Now()
	if got := recoveryGate(cfg, 3, now.Add(-24*time.Hour), now); got != recoveryExhausted {
		t.Errorf("attempts=3 max=3 = %v, want recoveryExhausted", got)
	}
}

func TestRecoveryGate_ZeroMaxAttemptsMeansUnlimited(t *testing.T) {
	cfg := heartbeatCfg()
	cfg.RecoveryMaxAttempts = 0
	now := time.Now()
	if got := recoveryGate(cfg, 99, now.Add(-16*time.Minute), now); got != recoveryRun {
		t.Errorf("max_attempts=0 with 99 prior attempts = %v, want recoveryRun (unlimited)", got)
	}
}

// Disabled must win over every other guard so the log line an operator sees
// names the real reason.
func TestRecoveryGate_DisabledTakesPrecedence(t *testing.T) {
	cfg := heartbeatCfg()
	cfg.RecoveryEnabled = false
	now := time.Now()
	if got := recoveryGate(cfg, 99, now, now); got != recoveryDisabled {
		t.Errorf("disabled + exhausted + cooling = %v, want recoveryDisabled", got)
	}
}

func TestResolveRecoveryCommand_ExplicitPath(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "deploy.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got, err := resolveRecoveryCommand(script)
	if err != nil {
		t.Fatalf("resolveRecoveryCommand(%q): %v", script, err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("resolved path %q is not absolute", got)
	}
	if filepath.Base(got) != "deploy.sh" {
		t.Errorf("resolved base = %q, want deploy.sh", filepath.Base(got))
	}
}

func TestResolveRecoveryCommand_MissingFileIsAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-there.sh")
	if _, err := resolveRecoveryCommand(missing); err == nil {
		t.Fatal("resolveRecoveryCommand on a missing file should error, so a typo surfaces as config, not as a failed exec mid-outage")
	}
}

func TestResolveRecoveryCommand_DirectoryIsAnError(t *testing.T) {
	dir := t.TempDir()
	if _, err := resolveRecoveryCommand(dir); err == nil {
		t.Fatal("resolveRecoveryCommand on a directory should error")
	}
}

// An empty RecoveryCommand falls back to deploy.sh beside the binary. The test
// binary has no deploy.sh next to it, so this must be a clean error rather than
// a panic or an empty path handed to exec.
func TestResolveRecoveryCommand_EmptyFallsBackToBinaryDir(t *testing.T) {
	got, err := resolveRecoveryCommand("")
	if err != nil {
		if got != "" {
			t.Errorf("error path returned a non-empty command %q", got)
		}
		return
	}
	if filepath.Base(got) != recoveryCommandName {
		t.Errorf("fallback resolved to %q, want a path ending in %q", got, recoveryCommandName)
	}
}

// startHeartbeat must be a no-op when disabled, and stopHeartbeat must remain
// safe to call regardless, so Stop() does not block or panic on a module whose
// heartbeat never started.
func TestStartHeartbeat_DisabledIsNoOpAndStopIsSafe(t *testing.T) {
	m := NewModule(nil)
	cfg := heartbeatCfg()
	cfg.HeartbeatEnabled = false

	m.startHeartbeat(cfg)

	m.bgMu.Lock()
	cancel := m.bgCancel
	m.bgMu.Unlock()
	if cancel != nil {
		t.Error("startHeartbeat set a cancel func while disabled; nothing should be running")
	}

	done := make(chan struct{})
	go func() {
		m.stopHeartbeat()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stopHeartbeat blocked when no heartbeat was started")
	}
}

// A started heartbeat must be joinable. With no pool configured every tick
// fails, which also exercises the nil-pool path in heartbeatPing.
func TestStopHeartbeat_JoinsRunningLoop(t *testing.T) {
	m := NewModule(nil)
	cfg := heartbeatCfg()
	cfg.HeartbeatInterval = 10 * time.Millisecond
	cfg.RecoveryEnabled = false // never exec anything from a test

	m.startHeartbeat(cfg)
	time.Sleep(50 * time.Millisecond) // let a few ticks fail

	done := make(chan struct{})
	go func() {
		m.stopHeartbeat()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stopHeartbeat did not join the heartbeat loop")
	}

	if m.IsConnected() {
		t.Error("IsConnected() is true after every heartbeat ping failed")
	}
}

func TestHeartbeatPing_NilPoolIsAFailure(t *testing.T) {
	m := NewModule(nil)
	if err := m.heartbeatPing(t.Context()); err == nil {
		t.Fatal("heartbeatPing with no pool should report a failure, not success")
	}
}
