package server

import (
	"context"
	"testing"

	"media-server-pro/internal/logger"
	"media-server-pro/pkg/models"
)

// ---------------------------------------------------------------------------
// CriticalModules
// ---------------------------------------------------------------------------

func TestCriticalModules_Contains(t *testing.T) {
	expected := []string{"database", "auth", "security", "media", "streaming", "tasks", "scanner", "thumbnails"}
	for _, name := range expected {
		if !CriticalModules[name] {
			t.Errorf("CriticalModules should contain %q", name)
		}
	}
}

func TestCriticalModules_ExcludesNonCritical(t *testing.T) {
	excluded := []string{"hls", "analytics", "playlist", "admin", "upload", "validator", "backup", "autodiscovery", "suggestions", "categorizer", "updater", "remote", "receiver"}
	for _, name := range excluded {
		if CriticalModules[name] {
			t.Errorf("CriticalModules should NOT contain %q", name)
		}
	}
}

// ---------------------------------------------------------------------------
// mockModule
// ---------------------------------------------------------------------------

type mockModule struct {
	name string
}

func (m *mockModule) Name() string                  { return m.name }
func (m *mockModule) Start(_ context.Context) error { return nil }
func (m *mockModule) Stop(_ context.Context) error  { return nil }
func (m *mockModule) Health() models.HealthStatus {
	return models.HealthStatus{Name: m.name, Status: "healthy", Message: "ok"}
}

// ---------------------------------------------------------------------------
// RegisterModule / GetModule
// ---------------------------------------------------------------------------

func TestRegisterModule_Success(t *testing.T) {
	s := &Server{
		log:        logger.New("test"),
		modules:    make([]Module, 0),
		modulesMap: make(map[string]Module),
	}
	err := s.RegisterModule(&mockModule{name: "test"})
	if err != nil {
		t.Fatalf("RegisterModule failed: %v", err)
	}
	if len(s.modules) != 1 {
		t.Errorf("expected 1 module, got %d", len(s.modules))
	}
}

func TestRegisterModule_Duplicate(t *testing.T) {
	s := &Server{
		log:        logger.New("test"),
		modules:    make([]Module, 0),
		modulesMap: make(map[string]Module),
	}
	s.RegisterModule(&mockModule{name: "test"})
	err := s.RegisterModule(&mockModule{name: "test"})
	if err == nil {
		t.Error("duplicate module should be rejected")
	}
}

func TestGetModule_Found(t *testing.T) {
	s := &Server{
		log:        logger.New("test"),
		modules:    make([]Module, 0),
		modulesMap: make(map[string]Module),
	}
	s.RegisterModule(&mockModule{name: "mymod"})
	m, ok := s.GetModule("mymod")
	if !ok || m == nil {
		t.Error("should find registered module")
	}
}

func TestGetModule_NotFound(t *testing.T) {
	s := &Server{
		log:        logger.New("test"),
		modules:    make([]Module, 0),
		modulesMap: make(map[string]Module),
	}
	_, ok := s.GetModule("nonexistent")
	if ok {
		t.Error("should not find unregistered module")
	}
}

// ---------------------------------------------------------------------------
// createTLSConfig
// ---------------------------------------------------------------------------

func TestCreateTLSConfig_EmptyCert(t *testing.T) {
	_, err := createTLSConfig("", "key.pem")
	if err == nil {
		t.Error("empty cert should return error")
	}
}

func TestCreateTLSConfig_EmptyKey(t *testing.T) {
	_, err := createTLSConfig("cert.pem", "")
	if err == nil {
		t.Error("empty key should return error")
	}
}

func TestCreateTLSConfig_MissingFiles(t *testing.T) {
	_, err := createTLSConfig("/nonexistent/cert.pem", "/nonexistent/key.pem")
	if err == nil {
		t.Error("missing cert file should return error")
	}
}
