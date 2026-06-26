package runtimeenv

import (
	"testing"

	"media-server-pro/internal/logger"
)

func TestUsableCPUs_AtLeastOne(t *testing.T) {
	if n := UsableCPUs(); n < 1 {
		t.Fatalf("UsableCPUs() = %d, want >= 1", n)
	}
}

func TestLogCPUProfile_DoesNotPanic(t *testing.T) {
	// Smoke test: the startup log helper must be safe to call with a real logger.
	LogCPUProfile(logger.New("test"))
}
