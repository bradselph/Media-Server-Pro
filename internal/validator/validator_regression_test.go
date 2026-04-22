package validator

import (
	"testing"
	"time"
)

// FND-0021/0022/0023: Regression tests ensuring nil repo does not panic.
// Before the fix, calling storeResult/loadResults/saveResults with m.repo == nil
// caused a nil pointer dereference panic.

func newModuleNoRepo() *Module {
	return &Module{
		results: make(map[string]*ValidationResult),
		fixing:  make(map[string]bool),
		log:     nil, // logger can be nil for these tests; storeResult returns early
		// repo intentionally left nil
	}
}

func TestFND0021_StoreResult_NilRepo_NoPanic(t *testing.T) {
	m := newModuleNoRepo()
	result := &ValidationResult{
		Path:        "/test/video.mp4",
		Status:      StatusValidated,
		ValidatedAt: time.Now(),
	}
	// Must not panic when repo is nil
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("storeResult panicked with nil repo: %v (FND-0021 regression)", r)
		}
	}()
	m.storeResult(result)
	// Result should still be stored in-memory
	if _, ok := m.results[result.Path]; !ok {
		t.Error("storeResult should still update in-memory results when repo is nil")
	}
}

func TestFND0023_LoadResults_NilRepo_ReturnsNil(t *testing.T) {
	m := newModuleNoRepo()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("loadResults panicked with nil repo: %v (FND-0023 regression)", r)
		}
	}()
	err := m.loadResults()
	if err != nil {
		t.Errorf("loadResults with nil repo should return nil, got: %v (FND-0023 regression)", err)
	}
}

func TestFND0022_SaveResults_NilRepo_ReturnsNil(t *testing.T) {
	m := newModuleNoRepo()
	m.results["/test/a.mp4"] = &ValidationResult{Path: "/test/a.mp4", Status: StatusValidated}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("saveResults panicked with nil repo: %v (FND-0022 regression)", r)
		}
	}()
	err := m.saveResults()
	if err != nil {
		t.Errorf("saveResults with nil repo should return nil, got: %v (FND-0022 regression)", err)
	}
}
