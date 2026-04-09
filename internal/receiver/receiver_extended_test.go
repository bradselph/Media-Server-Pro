package receiver

import (
	"testing"
	"time"

	"media-server-pro/internal/repositories"
)

// ---------------------------------------------------------------------------
// slaveRecordToNode
// ---------------------------------------------------------------------------

func TestSlaveRecordToNode(t *testing.T) {
	now := time.Now()
	rec := &repositories.ReceiverSlaveRecord{
		ID:         "slave-1",
		Name:       "Node A",
		BaseURL:    "http://10.0.0.1:8080",
		Status:     "online",
		MediaCount: 42,
		LastSeen:   now,
		CreatedAt:  now.Add(-24 * time.Hour),
	}
	node := slaveRecordToNode(rec)
	if node.ID != "slave-1" {
		t.Errorf("ID = %q", node.ID)
	}
	if node.Name != "Node A" {
		t.Errorf("Name = %q", node.Name)
	}
	if node.BaseURL != "http://10.0.0.1:8080" {
		t.Errorf("BaseURL = %q", node.BaseURL)
	}
	if node.Status != "online" {
		t.Errorf("Status = %q", node.Status)
	}
	if node.MediaCount != 42 {
		t.Errorf("MediaCount = %d", node.MediaCount)
	}
	if !node.RegisteredAt.Equal(rec.CreatedAt) {
		t.Error("RegisteredAt should map from CreatedAt")
	}
}

// ---------------------------------------------------------------------------
// nodeToSlaveRecord
// ---------------------------------------------------------------------------

func TestNodeToSlaveRecord(t *testing.T) {
	now := time.Now()
	node := &SlaveNode{
		ID:           "slave-2",
		Name:         "Node B",
		BaseURL:      "http://10.0.0.2:8080",
		Status:       "offline",
		MediaCount:   100,
		LastSeen:     now,
		RegisteredAt: now.Add(-48 * time.Hour),
	}
	rec := nodeToSlaveRecord(node)
	if rec.ID != "slave-2" {
		t.Errorf("ID = %q", rec.ID)
	}
	if rec.Name != "Node B" {
		t.Errorf("Name = %q", rec.Name)
	}
	if !rec.CreatedAt.Equal(node.RegisteredAt) {
		t.Error("CreatedAt should map from RegisteredAt")
	}
}

// ---------------------------------------------------------------------------
// Round-trip: record -> node -> record
// ---------------------------------------------------------------------------

func TestSlaveRecordRoundTrip(t *testing.T) {
	now := time.Now()
	original := &repositories.ReceiverSlaveRecord{
		ID:         "rt-1",
		Name:       "RoundTrip",
		BaseURL:    "http://10.0.0.3",
		Status:     "degraded",
		MediaCount: 7,
		LastSeen:   now,
		CreatedAt:  now.Add(-1 * time.Hour),
	}
	node := slaveRecordToNode(original)
	restored := nodeToSlaveRecord(node)

	if restored.ID != original.ID {
		t.Errorf("ID mismatch: %q vs %q", restored.ID, original.ID)
	}
	if restored.Name != original.Name {
		t.Error("Name mismatch")
	}
	if restored.BaseURL != original.BaseURL {
		t.Error("BaseURL mismatch")
	}
	if restored.Status != original.Status {
		t.Error("Status mismatch")
	}
}

// ---------------------------------------------------------------------------
// mediaRecordToItem
// ---------------------------------------------------------------------------

func TestMediaRecordToItem(t *testing.T) {
	rec := &repositories.ReceiverMediaRecord{
		ID:                 "media-1",
		SlaveID:            "slave-1",
		RemotePath:         "/remote/video.mp4",
		Name:               "video.mp4",
		MediaType:          "video",
		Size:               1024000,
		Duration:           120.5,
		ContentType:        "video/mp4",
		ContentFingerprint: "abc123",
		Width:              1920,
		Height:             1080,
	}
	item := mediaRecordToItem(rec)
	if item.ID != "media-1" {
		t.Errorf("ID = %q", item.ID)
	}
	if item.SlaveID != "slave-1" {
		t.Errorf("SlaveID = %q", item.SlaveID)
	}
	if item.Path != "/remote/video.mp4" {
		t.Errorf("Path = %q (should map from RemotePath)", item.Path)
	}
	if item.Name != "video.mp4" {
		t.Errorf("Name = %q", item.Name)
	}
	if item.Size != 1024000 {
		t.Errorf("Size = %d", item.Size)
	}
	if item.Width != 1920 || item.Height != 1080 {
		t.Errorf("Dimensions = %dx%d", item.Width, item.Height)
	}
}
