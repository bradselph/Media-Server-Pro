package hls

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"media-server-pro/internal/logger"
	"media-server-pro/pkg/models"
)

// stubHLSRepo is a minimal HLSJobRepository used to exercise save-error paths.
type stubHLSRepo struct {
	saveErr error
}

func (s *stubHLSRepo) Save(_ context.Context, _ *models.HLSJob) error          { return s.saveErr }
func (s *stubHLSRepo) Get(_ context.Context, _ string) (*models.HLSJob, error) { return nil, nil }
func (s *stubHLSRepo) Delete(_ context.Context, _ string) error                { return nil }
func (s *stubHLSRepo) List(_ context.Context) ([]*models.HLSJob, error)        { return nil, nil }

// TestSaveJob_ReturnsError confirms saveJob now surfaces the repo error (and nil on success).
func TestSaveJob_ReturnsError(t *testing.T) {
	fail := &Module{repo: &stubHLSRepo{saveErr: errors.New("db down")}, log: logger.New("test")}
	if err := fail.saveJob(&models.HLSJob{ID: "j1"}); err == nil {
		t.Error("saveJob should return the underlying repo error")
	}
	ok := &Module{repo: &stubHLSRepo{}, log: logger.New("test")}
	if err := ok.saveJob(&models.HLSJob{ID: "j1"}); err != nil {
		t.Errorf("saveJob should return nil on success, got %v", err)
	}
}

// TestCancelJob_SaveError_DoesNotPropagate confirms a failed status persist is
// logged (as a warning) but does not fail CancelJob, and that the in-memory
// status is still flipped to Canceled.
func TestCancelJob_SaveError_DoesNotPropagate(t *testing.T) {
	m := &Module{
		repo:       &stubHLSRepo{saveErr: errors.New("db down")},
		jobs:       map[string]*models.HLSJob{"j1": {ID: "j1", Status: models.HLSStatusRunning}},
		jobCancels: map[string]context.CancelFunc{},
		jobDone:    map[string]chan struct{}{},
		log:        logger.New("test"),
	}
	if err := m.CancelJob("j1"); err != nil {
		t.Fatalf("CancelJob must not propagate the save error, got %v", err)
	}
	if got := m.jobs["j1"].Status; got != models.HLSStatusCanceled {
		t.Errorf("in-memory status = %v, want Canceled", got)
	}
}

// TestDeleteJob_DrainsLazyTranscode confirms DeleteJob waits for an in-flight
// lazy transcode (tracked only in lazyWg, with no jobDone entry) to finish
// before os.RemoveAll deletes the output directory.
func TestDeleteJob_DrainsLazyTranscode(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "job1")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &Module{
		repo:       &stubHLSRepo{},
		jobs:       map[string]*models.HLSJob{"job1": {ID: "job1", OutputDir: outputDir, Status: models.HLSStatusCompleted}},
		jobCancels: map[string]context.CancelFunc{},
		jobDone:    map[string]chan struct{}{}, // no jobDone entry — this is the lazy-transcode case
		log:        logger.New("test"),
	}

	// Simulate an in-flight lazy transcode holding the per-job WaitGroup.
	rawWg, _ := m.lazyWg.LoadOrStore("job1", new(sync.WaitGroup))
	wg := rawWg.(*sync.WaitGroup)
	wg.Add(1)

	var mu sync.Mutex
	var order []string
	done := make(chan struct{})
	go func() {
		time.Sleep(80 * time.Millisecond)
		mu.Lock()
		order = append(order, "lazy-done")
		mu.Unlock()
		wg.Done() // must unblock DeleteJob's drain
		close(done)
	}()

	if err := m.DeleteJob("job1"); err != nil {
		t.Fatalf("DeleteJob: %v", err)
	}
	mu.Lock()
	order = append(order, "delete-done")
	mu.Unlock()

	<-done
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "lazy-done" {
		t.Errorf("DeleteJob did not wait for the lazy transcode; order=%v", order)
	}
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Errorf("output dir should have been removed by DeleteJob, stat err=%v", err)
	}
}

// TestDeleteJob_CancelsLazyTranscode confirms DeleteJob cancels an in-flight
// lazy transcode (rather than blocking on it): the simulated transcode only
// finishes when its registered context is cancelled, so if cancellation didn't
// fire, DeleteJob would hang and the test would time out.
func TestDeleteJob_CancelsLazyTranscode(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "job1")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &Module{
		repo:       &stubHLSRepo{},
		jobs:       map[string]*models.HLSJob{"job1": {ID: "job1", OutputDir: outputDir, Status: models.HLSStatusCompleted}},
		jobCancels: map[string]context.CancelFunc{},
		jobDone:    map[string]chan struct{}{},
		log:        logger.New("test"),
	}

	// Simulate an in-flight lazy transcode: holds lazyWg and blocks on its
	// registered cancellable context (as real ffmpeg does via exec.CommandContext).
	rawWg, _ := m.lazyWg.LoadOrStore("job1", new(sync.WaitGroup))
	wg := rawWg.(*sync.WaitGroup)
	wg.Add(1)
	ctx, cancel := context.WithCancel(context.Background())
	rawSet, _ := m.lazyCancels.LoadOrStore("job1", &sync.Map{})
	rawSet.(*sync.Map).Store(new(int), context.CancelFunc(cancel))

	started := make(chan struct{})
	go func() {
		close(started)
		<-ctx.Done() // only returns once DeleteJob cancels us
		wg.Done()
	}()
	<-started

	done := make(chan error, 1)
	go func() { done <- m.DeleteJob("job1") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("DeleteJob: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("DeleteJob hung — cancellation of the lazy transcode did not unblock the drain")
	}
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Errorf("output dir should have been removed by DeleteJob, stat err=%v", err)
	}
}

// ---------------------------------------------------------------------------
// isSegmentLine
// ---------------------------------------------------------------------------

func TestIsSegmentLine(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"segment001.ts", true},
		{"audio.aac", true},
		{"chunk.m4s", true},
		{"init.mp4", true},
		{"", false},
		{"#EXT-X-STREAM-INF:", false},
		{"#EXTINF:10.0,", false},
		{"readme.txt", false},
		{"video.mkv", false},
	}
	for _, tc := range tests {
		got := isSegmentLine(tc.line)
		if got != tc.want {
			t.Errorf("isSegmentLine(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// rewritePlaylistLines
// ---------------------------------------------------------------------------

func TestRewritePlaylistLines_Simple(t *testing.T) {
	data := []byte("#EXTM3U\n#EXTINF:10.0,\nseg001.ts\n#EXTINF:10.0,\nseg002.ts\n")
	got := string(rewritePlaylistLines(data, "/hls/job123/720p/"))
	if got == "" {
		t.Fatal("result should not be empty")
	}
	// Comment lines should be preserved as-is
	if !strings.Contains(got, "#EXTM3U") {
		t.Error("should preserve #EXTM3U tag")
	}
	// Segment lines should be rewritten with base URL
	if !strings.Contains(got, "/hls/job123/720p/seg001.ts") {
		t.Errorf("should rewrite segment URI, got:\n%s", got)
	}
	if !strings.Contains(got, "/hls/job123/720p/seg002.ts") {
		t.Errorf("should rewrite second segment URI, got:\n%s", got)
	}
}

func TestRewritePlaylistLines_PreservesComments(t *testing.T) {
	data := []byte("#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:10.0,\nseg.ts\n#EXT-X-ENDLIST\n")
	got := string(rewritePlaylistLines(data, "/base/"))
	if !strings.Contains(got, "#EXT-X-VERSION:3") {
		t.Error("should preserve version tag")
	}
	if !strings.Contains(got, "#EXT-X-ENDLIST") {
		t.Error("should preserve endlist tag")
	}
}

func TestRewritePlaylistLines_EmptyInput(_ *testing.T) {
	// nil input may produce empty or single newline, both OK — just verify no panic
	_ = rewritePlaylistLines(nil, "/base/")
}

// ---------------------------------------------------------------------------
// copyHLSJob
// ---------------------------------------------------------------------------

func TestCopyHLSJob_Nil(t *testing.T) {
	got := copyHLSJob(nil)
	if got != nil {
		t.Error("copyHLSJob(nil) should return nil")
	}
}

func TestCopyHLSJob_DeepCopy(t *testing.T) {
	original := &models.HLSJob{
		ID:          "job-1",
		MediaPath:   "/test/media-1.mp4",
		Status:      models.HLSStatusCompleted,
		Qualities:   []string{"720p", "1080p"},
		CompletedAt: new(time.Now()),
	}
	cp := copyHLSJob(original)
	if cp == original {
		t.Error("should return a different pointer")
	}
	if cp.ID != "job-1" {
		t.Errorf("ID = %q", cp.ID)
	}
	if len(cp.Qualities) != 2 {
		t.Fatal("qualities should be copied")
	}
	// Mutate copy — original should be unaffected
	cp.Qualities[0] = "480p"
	if original.Qualities[0] != "720p" {
		t.Error("mutating copy should not affect original qualities")
	}
	// CompletedAt should be independent
	if cp.CompletedAt == original.CompletedAt {
		t.Error("CompletedAt pointer should be independent")
	}
}

func TestCopyHLSJob_NilTimeFields(t *testing.T) {
	original := &models.HLSJob{
		ID:          "job-2",
		Qualities:   []string{"720p"},
		CompletedAt: nil,
	}
	cp := copyHLSJob(original)
	if cp.CompletedAt != nil {
		t.Error("nil CompletedAt should stay nil in copy")
	}
}

// ---------------------------------------------------------------------------
// isJobRunningOrPending
// ---------------------------------------------------------------------------

func TestIsJobRunningOrPending(t *testing.T) {
	tests := []struct {
		name   string
		job    *models.HLSJob
		exists bool
		want   bool
	}{
		{"nil job", nil, false, false},
		{"not exists", &models.HLSJob{Status: models.HLSStatusRunning}, false, false},
		{"running", &models.HLSJob{Status: models.HLSStatusRunning}, true, true},
		{"pending", &models.HLSJob{Status: models.HLSStatusPending}, true, true},
		{"completed", &models.HLSJob{Status: models.HLSStatusCompleted}, true, false},
		{"failed", &models.HLSJob{Status: models.HLSStatusFailed}, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isJobRunningOrPending(tc.job, tc.exists)
			if got != tc.want {
				t.Errorf("isJobRunningOrPending = %v, want %v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseVariantStreams (method on *Module)
// ---------------------------------------------------------------------------

func TestParseVariantStreams(t *testing.T) {
	m := &Module{log: logger.New("test")}
	content := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=640x360
360p/playlist.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2000000,RESOLUTION=1280x720
720p/playlist.m3u8
`
	variants := m.parseVariantStreams(content)
	if len(variants) != 2 {
		t.Fatalf("expected 2 variants, got %d", len(variants))
	}
	if variants[0] != "360p/playlist.m3u8" {
		t.Errorf("variant[0] = %q", variants[0])
	}
	if variants[1] != "720p/playlist.m3u8" {
		t.Errorf("variant[1] = %q", variants[1])
	}
}

func TestParseVariantStreams_Empty(t *testing.T) {
	m := &Module{log: logger.New("test")}
	variants := m.parseVariantStreams("")
	if len(variants) != 0 {
		t.Errorf("empty content should produce 0 variants, got %d", len(variants))
	}
}

func TestParseVariantStreams_WindowsLineEndings(t *testing.T) {
	m := &Module{log: logger.New("test")}
	content := "#EXTM3U\r\n#EXT-X-STREAM-INF:BANDWIDTH=800000\r\n360p/playlist.m3u8\r\n"
	variants := m.parseVariantStreams(content)
	if len(variants) != 1 {
		t.Fatalf("expected 1 variant with CRLF, got %d", len(variants))
	}
}

// ---------------------------------------------------------------------------
// parseSegments (method on *Module)
// ---------------------------------------------------------------------------

func TestParseSegments(t *testing.T) {
	m := &Module{log: logger.New("test")}
	content := `#EXTM3U
#EXT-X-VERSION:3
#EXTINF:10.0,
seg001.ts
#EXTINF:10.0,
seg002.ts
#EXT-X-ENDLIST
`
	segments := m.parseSegments(content)
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	if segments[0] != "seg001.ts" {
		t.Errorf("segment[0] = %q", segments[0])
	}
}

func TestParseSegments_Empty(t *testing.T) {
	m := &Module{log: logger.New("test")}
	segments := m.parseSegments("")
	if len(segments) != 0 {
		t.Errorf("empty should produce 0 segments, got %d", len(segments))
	}
}

// ---------------------------------------------------------------------------
// parseProbeDuration (method on *Module)
// ---------------------------------------------------------------------------

func TestParseProbeDuration_Valid(t *testing.T) {
	m := &Module{log: logger.New("test")}
	json := `{"format":{"duration":"120.500000"}}`
	got := m.parseProbeDuration(json)
	if got < 120.4 || got > 120.6 {
		t.Errorf("parseProbeDuration = %f, want ~120.5", got)
	}
}

func TestParseProbeDuration_InvalidJSON(t *testing.T) {
	m := &Module{log: logger.New("test")}
	got := m.parseProbeDuration("not json")
	if got != 0 {
		t.Errorf("invalid JSON should return 0, got %f", got)
	}
}

func TestParseProbeDuration_MissingField(t *testing.T) {
	m := &Module{log: logger.New("test")}
	got := m.parseProbeDuration(`{"format":{}}`)
	if got != 0 {
		t.Errorf("missing duration should return 0, got %f", got)
	}
}

// ---------------------------------------------------------------------------
// parseProbeHeight (method on *Module)
// ---------------------------------------------------------------------------

func TestParseProbeHeight_Valid(t *testing.T) {
	m := &Module{log: logger.New("test")}
	json := `{"streams":[{"codec_type":"video","height":1080},{"codec_type":"audio","height":0}]}`
	got := m.parseProbeHeight(json)
	if got != 1080 {
		t.Errorf("parseProbeHeight = %d, want 1080", got)
	}
}

func TestParseProbeHeight_NoVideo(t *testing.T) {
	m := &Module{log: logger.New("test")}
	json := `{"streams":[{"codec_type":"audio","height":0}]}`
	got := m.parseProbeHeight(json)
	if got != 0 {
		t.Errorf("no video stream should return 0, got %d", got)
	}
}

func TestParseProbeHeight_InvalidJSON(t *testing.T) {
	m := &Module{log: logger.New("test")}
	got := m.parseProbeHeight("bad")
	if got != 0 {
		t.Errorf("invalid JSON should return 0, got %d", got)
	}
}
