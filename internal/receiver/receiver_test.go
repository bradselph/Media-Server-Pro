package receiver

import (
	"path/filepath"
	"testing"

	"media-server-pro/internal/config"
)

// ---------------------------------------------------------------------------
// opaqueMediaID
// ---------------------------------------------------------------------------

func TestOpaqueMediaID_Deterministic(t *testing.T) {
	id1 := opaqueMediaID("slave-1", "item-1")
	id2 := opaqueMediaID("slave-1", "item-1")
	if id1 != id2 {
		t.Error("same inputs should produce same opaque ID")
	}
}

func TestOpaqueMediaID_Length(t *testing.T) {
	id := opaqueMediaID("slave-1", "item-1")
	if len(id) != 32 {
		t.Errorf("opaque ID should be 32 hex chars, got %d", len(id))
	}
}

func TestOpaqueMediaID_DifferentSlaves(t *testing.T) {
	id1 := opaqueMediaID("slave-1", "item-1")
	id2 := opaqueMediaID("slave-2", "item-1")
	if id1 == id2 {
		t.Error("different slaves should produce different IDs")
	}
}

func TestOpaqueMediaID_DifferentItems(t *testing.T) {
	id1 := opaqueMediaID("slave-1", "item-1")
	id2 := opaqueMediaID("slave-1", "item-2")
	if id1 == id2 {
		t.Error("different items should produce different IDs")
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "receiver" {
		t.Errorf("Name() = %q, want %q", m.Name(), "receiver")
	}
}

func TestModuleHealth_Default(t *testing.T) {
	m := &Module{}
	h := m.Health()
	if h.Name != "receiver" {
		t.Errorf("Health().Name = %q", h.Name)
	}
}

func TestSetHealth(t *testing.T) {
	m := &Module{}
	m.setHealth(true, "Running with 5 slaves")
	h := m.Health()
	if h.Status != "healthy" {
		t.Errorf("status = %q, want healthy", h.Status)
	}
	if h.Message != "Running with 5 slaves" {
		t.Errorf("message = %q", h.Message)
	}
}

// ---------------------------------------------------------------------------
// ValidateAPIKey
// ---------------------------------------------------------------------------

func TestValidateAPIKey_Empty(t *testing.T) {
	cfg := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	m := &Module{config: cfg}
	if m.ValidateAPIKey("") {
		t.Error("empty key should be rejected")
	}
}

func TestValidateAPIKey_NoConfiguredKeys(t *testing.T) {
	cfg := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	m := &Module{config: cfg}
	if m.ValidateAPIKey("some-key") {
		t.Error("should reject when no keys configured")
	}
}

func TestValidateAPIKey_ValidKey(t *testing.T) {
	cfg := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	cfg.Update(func(c *config.Config) {
		c.Receiver.APIKeys = []string{"key-1", "key-2", "key-3"}
	})
	m := &Module{config: cfg}
	if !m.ValidateAPIKey("key-2") {
		t.Error("valid key should be accepted")
	}
}

func TestValidateAPIKey_InvalidKey(t *testing.T) {
	cfg := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	cfg.Update(func(c *config.Config) {
		c.Receiver.APIKeys = []string{"key-1", "key-2"}
	})
	m := &Module{config: cfg}
	if m.ValidateAPIKey("wrong-key") {
		t.Error("invalid key should be rejected")
	}
}

// ---------------------------------------------------------------------------
// Type structures
// ---------------------------------------------------------------------------

func TestRegisterRequest_Fields(t *testing.T) {
	req := RegisterRequest{
		SlaveID: "slave-1",
		Name:    "Test Slave",
		BaseURL: "https://slave.example.com",
	}
	if req.SlaveID != "slave-1" || req.Name != "Test Slave" || req.BaseURL != "https://slave.example.com" {
		t.Error("RegisterRequest fields should be set correctly")
	}
}

func TestCatalogItem_Fields(t *testing.T) {
	item := CatalogItem{
		ID:                 "item-1",
		Path:               "/videos/movie.mp4",
		Name:               "movie.mp4",
		MediaType:          "video",
		Size:               1024 * 1024,
		Duration:           120.5,
		ContentType:        "video/mp4",
		ContentFingerprint: "abc123",
		Width:              1920,
		Height:             1080,
	}
	if item.ID != "item-1" {
		t.Errorf("ID = %q", item.ID)
	}
	if item.Path != "/videos/movie.mp4" {
		t.Errorf("Path = %q", item.Path)
	}
	if item.Name != "movie.mp4" {
		t.Errorf("Name = %q", item.Name)
	}
	if item.MediaType != "video" {
		t.Errorf("MediaType = %q", item.MediaType)
	}
	if item.Size != 1024*1024 {
		t.Errorf("Size = %d", item.Size)
	}
	if item.Duration != 120.5 {
		t.Errorf("Duration = %v", item.Duration)
	}
	if item.ContentType != "video/mp4" {
		t.Errorf("ContentType = %q", item.ContentType)
	}
	if item.ContentFingerprint != "abc123" {
		t.Errorf("ContentFingerprint = %q", item.ContentFingerprint)
	}
	if item.Width != 1920 {
		t.Errorf("Width = %d", item.Width)
	}
	if item.Height != 1080 {
		t.Errorf("Height = %d", item.Height)
	}
}

func TestMediaItem_Fields(t *testing.T) {
	item := MediaItem{
		ID:        "opaque-id",
		SlaveID:   "slave-1",
		SlaveName: "Node A",
		Path:      "/videos/movie.mp4",
		Name:      "movie.mp4",
	}
	if item.ID != "opaque-id" {
		t.Errorf("ID = %q", item.ID)
	}
	if item.SlaveID != "slave-1" {
		t.Errorf("SlaveID = %q", item.SlaveID)
	}
	if item.SlaveName != "Node A" {
		t.Errorf("SlaveName = %q", item.SlaveName)
	}
	if item.Path != "/videos/movie.mp4" {
		t.Errorf("Path = %q", item.Path)
	}
	if item.Name != "movie.mp4" {
		t.Errorf("Name = %q", item.Name)
	}
}

func TestSlaveNode_Fields(t *testing.T) {
	node := SlaveNode{
		ID:         "slave-1",
		Name:       "Node A",
		BaseURL:    "https://slave.example.com",
		Status:     "online",
		MediaCount: 42,
	}
	if node.ID != "slave-1" {
		t.Errorf("ID = %q", node.ID)
	}
	if node.Name != "Node A" {
		t.Errorf("Name = %q", node.Name)
	}
	if node.BaseURL != "https://slave.example.com" {
		t.Errorf("BaseURL = %q", node.BaseURL)
	}
	if node.Status != "online" {
		t.Errorf("Status = %q", node.Status)
	}
	if node.MediaCount != 42 {
		t.Errorf("MediaCount = %d", node.MediaCount)
	}
}
