package autodiscovery

import (
	"os"
	"path/filepath"
	"testing"
)

const testVideoFilename = "video.mp4"

// ---------------------------------------------------------------------------
// padNumber
// ---------------------------------------------------------------------------

func TestPadNumber_SingleDigit(t *testing.T) {
	got := padNumber("3")
	if got != "03" {
		t.Errorf("padNumber('3') = %q, want '03'", got)
	}
}

func TestPadNumber_TwoDigits(t *testing.T) {
	got := padNumber("12")
	if got != "12" {
		t.Errorf("padNumber('12') = %q, want '12'", got)
	}
}

func TestPadNumber_ThreeDigits(t *testing.T) {
	got := padNumber("123")
	if got != "123" {
		t.Errorf("padNumber('123') = %q, want '123'", got)
	}
}

func TestPadNumber_Empty(t *testing.T) {
	got := padNumber("")
	if got != "" {
		t.Errorf("padNumber('') = %q, want ''", got)
	}
}

// ---------------------------------------------------------------------------
// isPathInAllowedDirs
// ---------------------------------------------------------------------------

func TestIsPathInAllowedDirs_Inside(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, testVideoFilename)
	err := os.WriteFile(path, []byte("test"), 0o600)
	if err != nil {
		return
	}

	absPath, _ := filepath.Abs(path)
	if !isPathInAllowedDirs(absPath, []string{dir}) {
		t.Error("path within allowed dir should return true")
	}
}

func TestIsPathInAllowedDirs_Outside(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	path := filepath.Join(dir2, testVideoFilename)
	absPath, _ := filepath.Abs(path)

	if isPathInAllowedDirs(absPath, []string{dir1}) {
		t.Error("path outside allowed dir should return false")
	}
}

func TestIsPathInAllowedDirs_EmptyDirs(t *testing.T) {
	if isPathInAllowedDirs("/some/path", nil) {
		t.Error("nil allowed dirs should return false")
	}
	if isPathInAllowedDirs("/some/path", []string{}) {
		t.Error("empty allowed dirs should return false")
	}
}

func TestIsPathInAllowedDirs_MultipleDirs(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	path := filepath.Join(dir2, testVideoFilename)
	absPath, _ := filepath.Abs(path)

	if !isPathInAllowedDirs(absPath, []string{dir1, dir2}) {
		t.Error("path in second allowed dir should return true")
	}
}

// ---------------------------------------------------------------------------
// Confidence type
// ---------------------------------------------------------------------------

func TestConfidence_Float64(t *testing.T) {
	c := Confidence(0.85)
	if c.Float64() != 0.85 {
		t.Errorf("Float64() = %f, want 0.85", c.Float64())
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "autodiscovery" {
		t.Errorf("Name() = %q, want %q", m.Name(), "autodiscovery")
	}
}

func TestModuleHealth_Default(t *testing.T) {
	m := &Module{}
	h := m.Health()
	if h.Name != "autodiscovery" {
		t.Errorf("Health().Name = %q", h.Name)
	}
}
