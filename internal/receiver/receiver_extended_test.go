package receiver

import (
	"context"
	"testing"
	"time"

	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
)

// ---------------------------------------------------------------------------
// slaveRecordToNode
// ---------------------------------------------------------------------------

func TestSlaveRecordToNode(t *testing.T) {
	now := time.Now()
	rec := &repositories.ReceiverSlaveRecord{
		ID:         testSlaveID1,
		Name:       "Node A",
		BaseURL:    "http://10.0.0.1:8080",
		Status:     "online",
		MediaCount: 42,
		LastSeen:   now,
		CreatedAt:  now.Add(-24 * time.Hour),
	}
	node := slaveRecordToNode(rec)
	if node.ID != testSlaveID1 {
		t.Errorf(testFmtID, node.ID)
	}
	if node.Name != "Node A" {
		t.Errorf(testFmtName, node.Name)
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
		t.Errorf(testFmtID, rec.ID)
	}
	if rec.Name != "Node B" {
		t.Errorf(testFmtName, rec.Name)
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
		SlaveID:            testSlaveID1,
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
		t.Errorf(testFmtID, item.ID)
	}
	if item.SlaveID != testSlaveID1 {
		t.Errorf("SlaveID = %q", item.SlaveID)
	}
	if item.Path != "/remote/video.mp4" {
		t.Errorf("Path = %q (should map from RemotePath)", item.Path)
	}
	if item.Name != "video.mp4" {
		t.Errorf(testFmtName, item.Name)
	}
	if item.Size != 1024000 {
		t.Errorf("Size = %d", item.Size)
	}
	if item.Width != 1920 || item.Height != 1080 {
		t.Errorf("Dimensions = %dx%d", item.Width, item.Height)
	}
}

// ---------------------------------------------------------------------------
// FND-0236 / FND-0239: RegisterSlave context parameter
// ---------------------------------------------------------------------------

// mockSlaveRepo is a minimal mock for ReceiverSlaveRepository used in regression tests.
type mockSlaveRepo struct {
	upsertCalls int
	lastCtx     context.Context
	shouldFail  bool
	failErr     error
}

func (m *mockSlaveRepo) Upsert(ctx context.Context, slave *repositories.ReceiverSlaveRecord) error {
	m.upsertCalls++
	m.lastCtx = ctx
	if m.shouldFail {
		return m.failErr
	}
	return nil
}

func (m *mockSlaveRepo) Get(ctx context.Context, slaveID string) (*repositories.ReceiverSlaveRecord, error) {
	return nil, nil
}

func (m *mockSlaveRepo) Delete(ctx context.Context, slaveID string) error {
	return nil
}

func (m *mockSlaveRepo) List(ctx context.Context) ([]*repositories.ReceiverSlaveRecord, error) {
	return nil, nil
}

// TestFND0239_RegisterSlave_AcceptsContext verifies RegisterSlave accepts and respects a context parameter.
// FND-0239 required changing RegisterSlave signature from func(req) to func(ctx context.Context, req)
// to allow callers (especially the WS read loop) to bound database operations with timeouts.
func TestFND0239_RegisterSlave_AcceptsContext(t *testing.T) {
	mock := &mockSlaveRepo{}
	m := &Module{
		log:       logger.New("receiver"),
		slaveRepo: mock,
		slaves:    make(map[string]*SlaveNode),
		media:     make(map[string]*MediaItem),
	}

	req := &RegisterRequest{
		Name:    "Test Slave",
		BaseURL: "https://example.com:8080",
	}

	ctx := context.Background()
	node, err := m.RegisterSlave(ctx, req)

	if err != nil {
		t.Errorf("RegisterSlave failed: %v", err)
	}
	if node == nil {
		t.Error("RegisterSlave should return a non-nil SlaveNode")
	}
	if mock.upsertCalls != 1 {
		t.Errorf("Upsert called %d times, expected 1", mock.upsertCalls)
	}
	// Verify the context was passed through to the repo
	if mock.lastCtx != ctx {
		t.Error("RegisterSlave should pass the supplied context to slaveRepo.Upsert")
	}
}

// TestFND0239_RegisterSlave_PropagatesCancelledContext verifies that when RegisterSlave is called
// with a canceled context, it propagates the cancellation error from the repo.
// This regression test ensures the fix for FND-0239 (which bounds DB operations with timeouts)
// works correctly: if the context is already canceled, the DB Upsert should fail immediately.
func TestFND0239_RegisterSlave_PropagatesCancelledContext(t *testing.T) {
	mock := &mockSlaveRepo{}
	mock.shouldFail = true
	mock.failErr = context.Canceled
	m := &Module{
		log:       logger.New("receiver"),
		slaveRepo: mock,
		slaves:    make(map[string]*SlaveNode),
		media:     make(map[string]*MediaItem),
	}

	req := &RegisterRequest{
		Name:    "Test Slave",
		BaseURL: "https://example.com:8080",
	}

	// Use an already-canceled context to simulate what happens when a WS read loop's
	// 5-second timeout (from FND-0239 fix) expires during a slow DB operation.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	node, err := m.RegisterSlave(ctx, req)

	if err == nil {
		t.Error("RegisterSlave should return an error when context is canceled")
	}
	if node != nil {
		t.Error("RegisterSlave should return nil SlaveNode on context cancellation")
	}
	if mock.upsertCalls != 1 {
		t.Errorf("Upsert called %d times, expected 1", mock.upsertCalls)
	}
}

// TestFND0236_maxCatalogPayloadBytes verifies the constant exists and has expected size.
// FND-0236 requires rejecting catalog payloads > 64 MiB before json.Unmarshal,
// preventing allocation spikes from crafted messages.
func TestFND0236_maxCatalogPayloadBytes(t *testing.T) {
	expectedSize := int64(64 * 1024 * 1024)
	if maxCatalogPayloadBytes != expectedSize {
		t.Errorf("maxCatalogPayloadBytes = %d, expected %d", maxCatalogPayloadBytes, expectedSize)
	}
}
