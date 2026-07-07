package follower

import (
	"context"
	"testing"
	"time"

	"media-server-pro/internal/logger"
)

// TestStopLoop_TimeoutKeepsGuard guards that when the running loop does NOT
// confirm exit before the deadline, stopLoop returns false and leaves
// cancel/loopDone intact — so Reload won't start a second loop (two WS sessions
// to the master) over one that's still shutting down.
func TestStopLoop_TimeoutKeepsGuard(t *testing.T) {
	m := &Module{log: logger.New("test")}
	m.loopMu.Lock()
	m.cancel = func() {}             // no-op; the fake loop never observes it
	m.loopDone = make(chan struct{}) // never closed -> simulates a stuck loop
	m.loopMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if m.stopLoop(ctx) {
		t.Fatal("stopLoop must return false when the loop does not exit before the deadline")
	}

	m.loopMu.Lock()
	stillGuarded := m.cancel != nil && m.loopDone != nil
	m.loopMu.Unlock()
	if !stillGuarded {
		t.Fatal("stopLoop must leave cancel/loopDone intact on timeout so startLoop's guard still blocks a second loop")
	}
}

// TestStopLoop_ConfirmedExitClearsGuard guards the happy path: once the loop has
// exited (loopDone closed), stopLoop returns true and clears the guard fields so
// a subsequent startLoop can proceed.
func TestStopLoop_ConfirmedExitClearsGuard(t *testing.T) {
	m := &Module{log: logger.New("test")}
	done := make(chan struct{})
	close(done) // loop already exited
	m.loopMu.Lock()
	m.cancel = func() {}
	m.loopDone = done
	m.loopMu.Unlock()

	if !m.stopLoop(context.Background()) {
		t.Fatal("stopLoop should return true when the loop has confirmed exit")
	}
	m.loopMu.Lock()
	cleared := m.cancel == nil && m.loopDone == nil
	m.loopMu.Unlock()
	if !cleared {
		t.Fatal("stopLoop should clear cancel/loopDone on confirmed exit")
	}
}
