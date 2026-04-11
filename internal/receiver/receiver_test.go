package receiver

import (
	"path/filepath"
	"testing"

	"media-server-pro/internal/config"
)

const (
	testItemID1       = "item-1"
	testSlaveID1      = "slave-1"
	testConfigJSON    = "config.json"
	testSlaveURL      = "https://slave.example.com"
	testVideoPath     = "/videos/movie.mp4"
	testMovieFilename = "movie.mp4"
	testFmtID         = "ID = %q"
	testFmtName       = "Name = %q"
	testNodeA         = "Node A"
)

// ---------------------------------------------------------------------------
// opaqueMediaID
// ---------------------------------------------------------------------------

func TestOpaqueMediaID_Deterministic(t *testing.T) {
	id1 := opaqueMediaID(testSlaveID1, testItemID1)
	id2 := opaqueMediaID(testSlaveID1, testItemID1)
	if id1 != id2 {
		t.Error("same inputs should produce same opaque ID")
	}
}

func TestOpaqueMediaID_Length(t *testing.T) {
	id := opaqueMediaID(testSlaveID1, testItemID1)
	if len(id) != 32 {
		t.Errorf("opaque ID should be 32 hex chars, got %d", len(id))
	}
}

func TestOpaqueMediaID_DifferentSlaves(t *testing.T) {
	id1 := opaqueMediaID(testSlaveID1, testItemID1)
	id2 := opaqueMediaID("slave-2", testItemID1)
	if id1 == id2 {
		t.Error("different slaves should produce different IDs")
	}
}

func TestOpaqueMediaID_DifferentItems(t *testing.T) {
	id1 := opaqueMediaID(testSlaveID1, testItemID1)
	id2 := opaqueMediaID(testSlaveID1, "item-2")
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
	cfg := config.NewManager(filepath.Join(t.TempDir(), testConfigJSON))
	m := &Module{config: cfg}
	if m.ValidateAPIKey("") {
		t.Error("empty key should be rejected")
	}
}

func TestValidateAPIKey_NoConfiguredKeys(t *testing.T) {
	cfg := config.NewManager(filepath.Join(t.TempDir(), testConfigJSON))
	m := &Module{config: cfg}
	if m.ValidateAPIKey("some-key") {
		t.Error("should reject when no keys configured")
	}
}

func TestValidateAPIKey_ValidKey(t *testing.T) {
	cfg := config.NewManager(filepath.Join(t.TempDir(), testConfigJSON))
	cfg.Update(func(c *config.Config) {
		c.Receiver.APIKeys = []string{"key-1", "key-2", "key-3"}
	})
	m := &Module{config: cfg}
	if !m.ValidateAPIKey("key-2") {
		t.Error("valid key should be accepted")
	}
}

func TestValidateAPIKey_InvalidKey(t *testing.T) {
	cfg := config.NewManager(filepath.Join(t.TempDir(), testConfigJSON))
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
		SlaveID: testSlaveID1,
		Name:    "Test Slave",
		BaseURL: testSlaveURL,
	}
	if req.SlaveID != testSlaveID1 || req.Name != "Test Slave" || req.BaseURL != testSlaveURL {
		t.Error("RegisterRequest fields should be set correctly")
	}
}

func TestCatalogItem_Fields(t *testing.T) {
	item := CatalogItem{
		ID:                 testItemID1,
		Path:               testVideoPath,
		Name:               testMovieFilename,
		MediaType:          "video",
		Size:               1024 * 1024,
		Duration:           120.5,
		ContentType:        "video/mp4",
		ContentFingerprint: "abc123",
		Width:              1920,
		Height:             1080,
	}
	if item.ID != testItemID1 {
		t.Errorf(testFmtID, item.ID)
	}
	if item.Path != testVideoPath {
		t.Errorf("Path = %q", item.Path)
	}
	if item.Name != testMovieFilename {
		t.Errorf(testFmtName, item.Name)
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
		SlaveID:   testSlaveID1,
		SlaveName: testNodeA,
		Path:      testVideoPath,
		Name:      testMovieFilename,
	}
	if item.ID != "opaque-id" {
		t.Errorf(testFmtID, item.ID)
	}
	if item.SlaveID != testSlaveID1 {
		t.Errorf("SlaveID = %q", item.SlaveID)
	}
	if item.SlaveName != testNodeA {
		t.Errorf("SlaveName = %q", item.SlaveName)
	}
	if item.Path != testVideoPath {
		t.Errorf("Path = %q", item.Path)
	}
	if item.Name != testMovieFilename {
		t.Errorf(testFmtName, item.Name)
	}
}

func TestSlaveNode_Fields(t *testing.T) {
	node := SlaveNode{
		ID:         testSlaveID1,
		Name:       testNodeA,
		BaseURL:    testSlaveURL,
		Status:     "online",
		MediaCount: 42,
	}
	if node.ID != testSlaveID1 {
		t.Errorf(testFmtID, node.ID)
	}
	if node.Name != testNodeA {
		t.Errorf(testFmtName, node.Name)
	}
	if node.BaseURL != testSlaveURL {
		t.Errorf("BaseURL = %q", node.BaseURL)
	}
	if node.Status != "online" {
		t.Errorf("Status = %q", node.Status)
	}
	if node.MediaCount != 42 {
		t.Errorf("MediaCount = %d", node.MediaCount)
	}
}
