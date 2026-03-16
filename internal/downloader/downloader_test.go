package downloader

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "downloader" {
		t.Errorf("Name() = %q, want %q", m.Name(), "downloader")
	}
}

func TestModuleHealth_Default(t *testing.T) {
	m := &Module{}
	h := m.Health()
	if h.Name != "downloader" {
		t.Errorf("Health().Name = %q", h.Name)
	}
}

func TestSetHealth(t *testing.T) {
	m := &Module{}
	m.setHealth(true, "Running")
	h := m.Health()
	if h.Status != "healthy" {
		t.Errorf("status = %q, want healthy", h.Status)
	}
	if h.Message != "Running" {
		t.Errorf("message = %q, want Running", h.Message)
	}
}

func TestSetHealth_Unhealthy(t *testing.T) {
	m := &Module{}
	m.setHealth(false, "Disconnected")
	h := m.Health()
	if h.Status != "unhealthy" {
		t.Errorf("status = %q, want unhealthy", h.Status)
	}
}
