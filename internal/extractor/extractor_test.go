package extractor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/repositories"
)

const (
	testExtractorStreamURL = "https://example.com/stream.m3u8"
	testSegmentFilename    = "segment001.ts"
	testStreamTitle        = "Test Stream"
)

type extractorRoundTripFunc func(*http.Request) (*http.Response, error)

func (f extractorRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type fakeExtractorItemRepository struct {
	mu                    sync.Mutex
	records               map[string]*repositories.ExtractorItemRecord
	upsertContextDeadline bool
	deleteContextDeadline bool
}

func newFakeExtractorItemRepository(records ...*repositories.ExtractorItemRecord) *fakeExtractorItemRepository {
	repo := &fakeExtractorItemRepository{records: make(map[string]*repositories.ExtractorItemRecord, len(records))}
	for _, record := range records {
		cp := *record
		repo.records[record.ID] = &cp
	}
	return repo
}

func (r *fakeExtractorItemRepository) Upsert(ctx context.Context, item *repositories.ExtractorItemRecord) error {
	_, hasDeadline := ctx.Deadline()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.upsertContextDeadline = hasDeadline
	cp := *item
	if existing := r.records[item.ID]; existing != nil {
		// Match the MySQL repository's conflict update: AddedBy and CreatedAt are
		// immutable and therefore retain their original values.
		cp.AddedBy = existing.AddedBy
		cp.CreatedAt = existing.CreatedAt
	}
	r.records[item.ID] = &cp
	return nil
}

func (r *fakeExtractorItemRepository) Get(_ context.Context, id string) (*repositories.ExtractorItemRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record := r.records[id]
	if record == nil {
		return nil, nil
	}
	cp := *record
	return &cp, nil
}

func (r *fakeExtractorItemRepository) Delete(ctx context.Context, id string) error {
	_, hasDeadline := ctx.Deadline()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleteContextDeadline = hasDeadline
	if _, exists := r.records[id]; !exists {
		return ErrNotFound
	}
	delete(r.records, id)
	return nil
}

func (r *fakeExtractorItemRepository) List(context.Context) ([]*repositories.ExtractorItemRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	records := make([]*repositories.ExtractorItemRecord, 0, len(r.records))
	for _, record := range r.records {
		cp := *record
		records = append(records, &cp)
	}
	return records, nil
}

func (r *fakeExtractorItemRepository) UpdateStatus(_ context.Context, id, status, errorMsg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	record := r.records[id]
	if record == nil {
		return ErrNotFound
	}
	record.Status = status
	record.ErrorMessage = errorMsg
	return nil
}

func (r *fakeExtractorItemRepository) deadlineFlags() (upsert, delete bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.upsertContextDeadline, r.deleteContextDeadline
}

func newExtractorTestConfig(t *testing.T, maxItems int) *config.Manager {
	t.Helper()
	cfg := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	if err := cfg.Update(func(c *config.Config) {
		c.Extractor.MaxItems = maxItems
	}); err != nil {
		t.Fatalf("configure extractor: %v", err)
	}
	return cfg
}

func successfulExtractorHTTPClient() *http.Client {
	return &http.Client{
		Transport: extractorRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("#EXTM3U\n")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
		Timeout: time.Second,
	}
}

// ---------------------------------------------------------------------------
// generateID
// ---------------------------------------------------------------------------

func TestGenerateID_Deterministic(t *testing.T) {
	id1 := generateID(testExtractorStreamURL)
	id2 := generateID(testExtractorStreamURL)
	if id1 != id2 {
		t.Error("same input should produce same ID")
	}
}

func TestGenerateID_Prefix(t *testing.T) {
	id := generateID(testExtractorStreamURL)
	if !strings.HasPrefix(id, "ext_") {
		t.Errorf("ID should start with 'ext_': %s", id)
	}
}

func TestGenerateID_DifferentInputs(t *testing.T) {
	id1 := generateID("https://example.com/a.m3u8")
	id2 := generateID("https://example.com/b.m3u8")
	if id1 == id2 {
		t.Error("different inputs should produce different IDs")
	}
}

// ---------------------------------------------------------------------------
// resolveBaseURL
// ---------------------------------------------------------------------------

func TestResolveBaseURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://cdn.example.com/hls/stream/master.m3u8", "https://cdn.example.com/hls/stream/"},
		{"https://cdn.example.com/video.m3u8", "https://cdn.example.com//"},
		{"https://cdn.example.com/path/to/playlist.m3u8?token=abc", "https://cdn.example.com/path/to/"},
	}
	for _, tc := range tests {
		got := resolveBaseURL(tc.input)
		if got != tc.want {
			t.Errorf("resolveBaseURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestResolveBaseURL_InvalidURL(t *testing.T) {
	// Should return the raw URL on parse failure
	got := resolveBaseURL("://bad")
	if got == "" {
		t.Error("should return something for invalid URL")
	}
}

// ---------------------------------------------------------------------------
// resolveURL
// ---------------------------------------------------------------------------

func TestResolveURL_AbsoluteURL(t *testing.T) {
	got := resolveURL("https://cdn.example.com/hls/", "https://other.com/segment.ts")
	if got != "https://other.com/segment.ts" {
		t.Errorf("absolute URL should be returned as-is: %s", got)
	}
}

func TestResolveURL_RelativeURL(t *testing.T) {
	got := resolveURL("https://cdn.example.com/hls/stream/", testSegmentFilename)
	if got != "https://cdn.example.com/hls/stream/segment001.ts" {
		t.Errorf("relative URL should be resolved: %s", got)
	}
}

func TestResolveURL_RootRelative(t *testing.T) {
	got := resolveURL("https://cdn.example.com/hls/stream/", "/absolute/segment.ts")
	if got != "https://cdn.example.com/absolute/segment.ts" {
		t.Errorf("root-relative URL should be resolved: %s", got)
	}
}

func TestResolveURL_HTTPPrefix(t *testing.T) {
	got := resolveURL("https://cdn.example.com/", "http://insecure.com/seg.ts")
	if got != "http://insecure.com/seg.ts" {
		t.Errorf("http:// URL should be returned as-is: %s", got)
	}
}

// ---------------------------------------------------------------------------
// extractSegmentFilename
// ---------------------------------------------------------------------------

func TestExtractSegmentFilename_Simple(t *testing.T) {
	got := extractSegmentFilename(testSegmentFilename)
	if got != testSegmentFilename {
		t.Errorf("extractSegmentFilename = %q, want %q", got, testSegmentFilename)
	}
}

func TestExtractSegmentFilename_WithPath(t *testing.T) {
	got := extractSegmentFilename("https://cdn.example.com/hls/segment001.ts")
	if got != testSegmentFilename {
		t.Errorf("extractSegmentFilename = %q, want %q", got, testSegmentFilename)
	}
}

func TestExtractSegmentFilename_WithQuery(t *testing.T) {
	got := extractSegmentFilename("https://cdn.example.com/hls/seg.ts?token=abc")
	if got != "seg.ts" {
		t.Errorf("extractSegmentFilename = %q, want %q", got, "seg.ts")
	}
}

func TestExtractSegmentFilename_EmptyPath(t *testing.T) {
	got := extractSegmentFilename("https://cdn.example.com/")
	// Should fallback to hash-based name
	if !strings.HasPrefix(got, "seg_") {
		t.Errorf("empty path should produce hash-based name: %s", got)
	}
}

// ---------------------------------------------------------------------------
// recordToItem / itemToRecord
// ---------------------------------------------------------------------------

func TestRecordToItem(t *testing.T) {
	now := time.Now()
	rec := &repositories.ExtractorItemRecord{
		ID:        "ext_abc123",
		Title:     testStreamTitle,
		StreamURL: testExtractorStreamURL,
		Status:    "active",
		AddedBy:   "admin",
		CreatedAt: now,
	}
	item := recordToItem(rec)
	if item.ID != "ext_abc123" {
		t.Errorf("ID = %q", item.ID)
	}
	if item.Title != testStreamTitle {
		t.Errorf("Title = %q", item.Title)
	}
	if item.Status != "active" {
		t.Errorf("Status = %q", item.Status)
	}
}

func TestItemToRecord(t *testing.T) {
	now := time.Now()
	item := &ExtractedItem{
		ID:        "ext_abc123",
		Title:     testStreamTitle,
		StreamURL: testExtractorStreamURL,
		Status:    "active",
		AddedBy:   "admin",
		CreatedAt: now,
	}
	rec := itemToRecord(item)
	if rec.ID != "ext_abc123" {
		t.Errorf("ID = %q", rec.ID)
	}
	if rec.StreamType != "hls" {
		t.Errorf("StreamType = %q, want hls", rec.StreamType)
	}
	if rec.SourceURL != item.StreamURL {
		t.Errorf("SourceURL = %q, want %q", rec.SourceURL, item.StreamURL)
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "extractor" {
		t.Errorf("Name() = %q, want %q", m.Name(), "extractor")
	}
}

func TestModuleHealth_Default(t *testing.T) {
	m := &Module{}
	h := m.Health()
	if h.Name != "extractor" {
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

// ---------------------------------------------------------------------------
// GetItem / GetAllItems / GetStats (in-memory operations)
// ---------------------------------------------------------------------------

func TestGetItem_Found(t *testing.T) {
	m := &Module{items: map[string]*ExtractedItem{
		"id1": {ID: "id1", Title: "Stream 1", Status: "active"},
	}}
	item := m.GetItem("id1")
	if item == nil {
		t.Fatal("expected item to be found")
	}
	if item.Title != "Stream 1" {
		t.Errorf("Title = %q", item.Title)
	}
}

func TestGetItem_NotFound(t *testing.T) {
	m := &Module{items: make(map[string]*ExtractedItem)}
	item := m.GetItem("nonexistent")
	if item != nil {
		t.Error("expected nil for nonexistent item")
	}
}

func TestGetAllItems(t *testing.T) {
	m := &Module{items: map[string]*ExtractedItem{
		"id1": {ID: "id1", Status: "active"},
		"id2": {ID: "id2", Status: "error"},
	}}
	items := m.GetAllItems()
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
}

func TestGetAllItems_Empty(t *testing.T) {
	m := &Module{items: make(map[string]*ExtractedItem)}
	items := m.GetAllItems()
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestGetStats(t *testing.T) {
	m := &Module{items: map[string]*ExtractedItem{
		"id1": {ID: "id1", Status: "active"},
		"id2": {ID: "id2", Status: "active"},
		"id3": {ID: "id3", Status: "error"},
	}}
	stats := m.GetStats()
	if stats.TotalItems != 3 {
		t.Errorf("TotalItems = %d, want 3", stats.TotalItems)
	}
	if stats.ActiveItems != 2 {
		t.Errorf("ActiveItems = %d, want 2", stats.ActiveItems)
	}
	if stats.ErrorItems != 1 {
		t.Errorf("ErrorItems = %d, want 1", stats.ErrorItems)
	}
}

// ---------------------------------------------------------------------------
// AddItem
// ---------------------------------------------------------------------------

func TestAddItem_UpdateAtCapacityPreservesImmutableFields(t *testing.T) {
	const streamURL = "https://8.8.8.8/existing.m3u8"
	id := generateID(streamURL)
	createdAt := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	existing := &ExtractedItem{
		ID:        id,
		Title:     "Original title",
		StreamURL: streamURL,
		Status:    "active",
		AddedBy:   "original-admin",
		CreatedAt: createdAt,
	}
	repo := newFakeExtractorItemRepository(itemToRecord(existing))
	m := NewModule(newExtractorTestConfig(t, 1), nil)
	m.repo = repo
	m.httpClient = successfulExtractorHTTPClient()
	m.items[id] = existing

	updated, err := m.AddItem(streamURL, "Updated title", "different-admin")
	if err != nil {
		t.Fatalf("AddItem update at capacity: %v", err)
	}
	if updated.Title != "Updated title" {
		t.Errorf("updated title = %q, want Updated title", updated.Title)
	}
	if updated.AddedBy != existing.AddedBy {
		t.Errorf("updated AddedBy = %q, want immutable %q", updated.AddedBy, existing.AddedBy)
	}
	if !updated.CreatedAt.Equal(createdAt) {
		t.Errorf("updated CreatedAt = %v, want immutable %v", updated.CreatedAt, createdAt)
	}

	cached := m.GetItem(id)
	persisted, err := repo.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("repo.Get: %v", err)
	}
	if cached == nil || persisted == nil {
		t.Fatal("expected item in both cache and repository")
	}
	if cached.AddedBy != persisted.AddedBy || !cached.CreatedAt.Equal(persisted.CreatedAt) {
		t.Fatalf("cache immutable fields (%q, %v) diverged from repository (%q, %v)",
			cached.AddedBy, cached.CreatedAt, persisted.AddedBy, persisted.CreatedAt)
	}
	upsertDeadline, _ := repo.deadlineFlags()
	if !upsertDeadline {
		t.Error("AddItem repository context must have a deadline")
	}
}

func TestAddItem_DoesNotHoldWriteLockDuringRemoteFetch(t *testing.T) {
	const (
		streamURL = "https://8.8.4.4/new.m3u8"
		removeID  = "existing-item"
	)
	repo := newFakeExtractorItemRepository(&repositories.ExtractorItemRecord{ID: removeID})
	m := NewModule(newExtractorTestConfig(t, 10), nil)
	m.repo = repo
	m.items[removeID] = &ExtractedItem{ID: removeID}

	fetchStarted := make(chan struct{})
	releaseFetch := make(chan struct{})
	m.httpClient = &http.Client{
		Transport: extractorRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			close(fetchStarted)
			select {
			case <-releaseFetch:
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("#EXTM3U\n")),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}),
		Timeout: 5 * time.Second,
	}

	addDone := make(chan error, 1)
	go func() {
		_, err := m.AddItem(streamURL, "New stream", "admin")
		addDone <- err
	}()

	select {
	case <-fetchStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("AddItem did not begin remote fetch")
	}

	removeDone := make(chan error, 1)
	go func() { removeDone <- m.RemoveItem(removeID) }()
	select {
	case err := <-removeDone:
		if err != nil {
			close(releaseFetch)
			<-addDone
			t.Fatalf("RemoveItem while AddItem fetched remotely: %v", err)
		}
	case <-time.After(2 * time.Second):
		close(releaseFetch)
		<-addDone
		<-removeDone
		t.Fatal("RemoveItem blocked behind AddItem remote fetch; writeMu is held across network I/O")
	}

	close(releaseFetch)
	if err := <-addDone; err != nil {
		t.Fatalf("AddItem after releasing fetch: %v", err)
	}
	upsertDeadline, deleteDeadline := repo.deadlineFlags()
	if !upsertDeadline {
		t.Error("AddItem repository context must have a deadline")
	}
	if !deleteDeadline {
		t.Error("RemoveItem repository context must have a deadline")
	}
}

// ---------------------------------------------------------------------------
// RemoveItem (in-memory only, no repo)
// ---------------------------------------------------------------------------

func TestRemoveItem_Exists(t *testing.T) {
	m := &Module{items: map[string]*ExtractedItem{
		"id1": {ID: "id1"},
	}}
	err := m.RemoveItem("id1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.GetItem("id1") != nil {
		t.Error("item should be removed")
	}
}

func TestRemoveItem_NotFound(t *testing.T) {
	m := &Module{items: make(map[string]*ExtractedItem)}
	err := m.RemoveItem("nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ErrNotFound
// ---------------------------------------------------------------------------

func TestErrNotFound(t *testing.T) {
	if ErrNotFound == nil {
		t.Error("ErrNotFound should not be nil")
	}
	if ErrNotFound.Error() != "extractor item not found" {
		t.Errorf("ErrNotFound.Error() = %q", ErrNotFound.Error())
	}
}

// ---------------------------------------------------------------------------
// playlistCacheTTL
// ---------------------------------------------------------------------------

func TestPlaylistCacheTTL(t *testing.T) {
	if playlistCacheTTL != 5*time.Minute {
		t.Errorf("playlistCacheTTL = %v, want 5m", playlistCacheTTL)
	}
}
