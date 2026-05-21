package main

import (
	"os"
	"regexp"
	"testing"
)

// TestVersionFlagShutdownOrder is a regression test for FND-0010. The
// --version exit branch in main() must call logger.Shutdown() before
// os.Exit(0), matching the order in fatalExit. main() invokes os.Exit
// directly, so we cannot exercise the branch from a unit test — instead
// we statically verify the source still contains the correct ordering.
func TestVersionFlagShutdownOrder(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	// Match the version branch (any indentation/whitespace), require
	// logger.Shutdown() ahead of os.Exit(0), and reject any other
	// statement sneaking in between them.
	pattern := regexp.MustCompile(`(?s)if\s+showVer\s*\{\s*showVersion\(\)\s*logger\.Shutdown\(\)\s*os\.Exit\(0\)\s*\}`)
	if !pattern.Match(src) {
		t.Fatal("FND-0010 regression: main.go --version branch must be `showVersion(); logger.Shutdown(); os.Exit(0)` in that order with nothing between")
	}
}

// TestFatalExitShutdownOrder verifies that fatalExit also flushes the
// logger before exiting (the consistency requirement from FND-0010).
func TestFatalExitShutdownOrder(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	pattern := regexp.MustCompile(`(?s)func\s+fatalExit\([^)]*\)\s*\{\s*log\.Error\([^)]*\)\s*logger\.Shutdown\(\)\s*os\.Exit\(1\)\s*\}`)
	if !pattern.Match(src) {
		t.Fatal("FND-0010 regression: fatalExit must call logger.Shutdown() between log.Error() and os.Exit(1)")
	}
}
