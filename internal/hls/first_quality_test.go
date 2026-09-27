package hls

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/pkg/models"
)

// ---------------------------------------------------------------------------
// H3: HLS becomes playable after the FIRST quality finishes, not only once
// the whole ladder is done. These tests cover: the master playlist being
// rewritten atomically to the correct subset after each quality; a job
// reporting playable while still Running; later-quality failure/cancel
// finalizing with the completed subset; and restart discovery/reuse accepting
// a partial ladder while ignoring/removing leftover in-progress variant dirs.
// ---------------------------------------------------------------------------

func newTestModuleWithConfig(t *testing.T) *Module {
	t.Helper()
	return &Module{
		log:    logger.New("test"),
		config: config.NewManager(filepath.Join(t.TempDir(), "config.json")),
	}
}

// ---------------------------------------------------------------------------
// generateMasterPlaylist: atomic rewrite to an exact subset
// ---------------------------------------------------------------------------

func TestGenerateMasterPlaylist_RewritesToExactSubsetEachTime(t *testing.T) {
	m := newTestModuleWithConfig(t)
	outputDir := t.TempDir()

	// First quality finishes: master must list exactly ["720p"], nothing else.
	if err := m.generateMasterPlaylist(&generateMasterPlaylistParams{OutputDir: outputDir, Variants: []string{"720p"}}); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	first, err := os.ReadFile(filepath.Join(outputDir, masterPlaylistName))
	if err != nil {
		t.Fatalf("read master: %v", err)
	}
	if !strings.Contains(string(first), "720p/playlist.m3u8") {
		t.Errorf("master after first quality = %q, want 720p listed", first)
	}
	if strings.Contains(string(first), "360p/playlist.m3u8") {
		t.Errorf("master after first quality = %q, want 360p NOT listed yet", first)
	}

	// Second quality finishes: master must now list both.
	if err := m.generateMasterPlaylist(&generateMasterPlaylistParams{OutputDir: outputDir, Variants: []string{"720p", "360p"}}); err != nil {
		t.Fatalf("second publish: %v", err)
	}
	second, err := os.ReadFile(filepath.Join(outputDir, masterPlaylistName))
	if err != nil {
		t.Fatalf("read master: %v", err)
	}
	if !strings.Contains(string(second), "720p/playlist.m3u8") || !strings.Contains(string(second), "360p/playlist.m3u8") {
		t.Fatalf("master after second quality = %q, want both 720p and 360p listed", second)
	}

	// The rewrite must be a replace, not an append: exactly one STREAM-INF per listed variant.
	if got := strings.Count(string(second), "#EXT-X-STREAM-INF"); got != 2 {
		t.Errorf("master has %d #EXT-X-STREAM-INF entries, want 2 (no duplicate/stale entries from the first write)", got)
	}

	// No temp files left behind after a successful publish.
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != masterPlaylistName {
			t.Errorf("unexpected leftover file in output dir after publish: %s", e.Name())
		}
	}
}

func TestGenerateMasterPlaylist_WriteFailure_LeavesNoMasterBehind(t *testing.T) {
	m := newTestModuleWithConfig(t)
	// outputDir points at a regular FILE, not a directory, so os.CreateTemp
	// inside generateMasterPlaylist fails deterministically (portable across
	// OSes) before any content is written or renamed into place.
	outputDir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(outputDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := m.generateMasterPlaylist(&generateMasterPlaylistParams{OutputDir: outputDir, Variants: []string{"720p"}})
	if err == nil {
		t.Fatal("expected an error when the output directory cannot be written to")
	}
	if _, statErr := os.Stat(filepath.Join(outputDir, masterPlaylistName)); statErr == nil {
		t.Error("master.m3u8 should not exist after a failed publish")
	}
}

// ---------------------------------------------------------------------------
// publishMasterPlaylist: first-quality vs. later-quality publish failure
// ---------------------------------------------------------------------------

func TestPublishMasterPlaylist_FirstQualityFails_MarksJobFailed(t *testing.T) {
	badOutputDir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(badOutputDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	job := &models.HLSJob{ID: "job1", OutputDir: badOutputDir, Status: models.HLSStatusRunning}
	m := &Module{
		repo:   &stubHLSRepo{},
		jobs:   map[string]*models.HLSJob{"job1": job},
		log:    logger.New("test"),
		config: config.NewManager(filepath.Join(t.TempDir(), "config.json")),
	}

	if ok := m.publishMasterPlaylist(job, []string{"720p"}); ok {
		t.Fatal("publishMasterPlaylist should report failure when the underlying write fails")
	}
	if job.Status != models.HLSStatusFailed {
		t.Errorf("job.Status = %v, want Failed — nothing was ever published for this job", job.Status)
	}
}

func TestPublishMasterPlaylist_LaterQualityFails_FinalizesCompletedSubset(t *testing.T) {
	badOutputDir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(badOutputDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	job := &models.HLSJob{ID: "job1", OutputDir: badOutputDir, Status: models.HLSStatusRunning, Qualities: []string{"360p", "720p"}}
	m := &Module{
		repo:       &stubHLSRepo{},
		jobs:       map[string]*models.HLSJob{"job1": job},
		jobCancels: map[string]context.CancelFunc{},
		log:        logger.New("test"),
		config:     config.NewManager(filepath.Join(t.TempDir(), "config.json")),
	}

	// 360p already published successfully (per the caller's contract, completed
	// only ever grows one entry at a time); the 720p publish now fails.
	if ok := m.publishMasterPlaylist(job, []string{"360p", "720p"}); ok {
		t.Fatal("publishMasterPlaylist should report failure when the underlying write fails")
	}
	if job.Status != models.HLSStatusCompleted {
		t.Errorf("job.Status = %v, want Completed (finalized with the last published subset)", job.Status)
	}
	if !slices.Equal(job.Qualities, []string{"360p"}) {
		t.Errorf("job.Qualities = %v, want [360p] — only what was actually published", job.Qualities)
	}
}

// ---------------------------------------------------------------------------
// finalizeAfterQualityFailure: first-quality / later-canceled / later-failed
// ---------------------------------------------------------------------------

func TestFinalizeAfterQualityFailure(t *testing.T) {
	tests := []struct {
		name           string
		startStatus    models.HLSStatus
		startAvailable bool
		completed      []string
		canceledCtx    bool
		wantStatus     models.HLSStatus
		wantAvailable  bool
		wantQualities  []string
	}{
		{
			name:           "first quality failure leaves status exactly as transcodeQuality set it",
			startStatus:    models.HLSStatusFailed,
			startAvailable: false,
			completed:      nil,
			wantStatus:     models.HLSStatusFailed,
			wantAvailable:  false,
			wantQualities:  []string{"360p", "720p"},
		},
		{
			name:           "later quality canceled keeps Canceled status and stays Available/usable",
			startStatus:    models.HLSStatusCanceled,
			startAvailable: true,
			completed:      []string{"360p"},
			canceledCtx:    true,
			wantStatus:     models.HLSStatusCanceled,
			wantAvailable:  true,
			wantQualities:  []string{"360p", "720p"}, // untouched — this path does not rewrite Qualities
		},
		{
			name:           "later quality genuinely failed finalizes Completed with the published subset",
			startStatus:    models.HLSStatusFailed,
			startAvailable: true,
			completed:      []string{"360p"},
			wantStatus:     models.HLSStatusCompleted,
			wantAvailable:  true,
			wantQualities:  []string{"360p"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			job := &models.HLSJob{ID: "job1", Status: tc.startStatus, Available: tc.startAvailable, Qualities: []string{"360p", "720p"}}
			m := &Module{
				repo:       &stubHLSRepo{},
				jobs:       map[string]*models.HLSJob{"job1": job},
				jobCancels: map[string]context.CancelFunc{},
				log:        logger.New("test"),
			}
			ctx := context.Background()
			if tc.canceledCtx {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			m.finalizeAfterQualityFailure(ctx, job, "720p", tc.completed)

			if job.Status != tc.wantStatus {
				t.Errorf("job.Status = %v, want %v", job.Status, tc.wantStatus)
			}
			if job.Available != tc.wantAvailable {
				t.Errorf("job.Available = %v, want %v", job.Available, tc.wantAvailable)
			}
			if !slices.Equal(job.Qualities, tc.wantQualities) {
				t.Errorf("job.Qualities = %v, want %v", job.Qualities, tc.wantQualities)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// markJobPlayable / hlsURLForJob
// ---------------------------------------------------------------------------

func TestHlsURLForJob(t *testing.T) {
	if got := hlsURLForJob("abc-123"); got != "/hls/abc-123/master.m3u8" {
		t.Errorf("hlsURLForJob(%q) = %q, want %q", "abc-123", got, "/hls/abc-123/master.m3u8")
	}
}

func TestMarkJobPlayable_SetsAvailableAndHLSUrl_LeavesStatusRunning(t *testing.T) {
	job := &models.HLSJob{ID: "job1", Status: models.HLSStatusRunning}
	m := &Module{
		repo: &stubHLSRepo{},
		jobs: map[string]*models.HLSJob{"job1": job},
		log:  logger.New("test"),
	}

	m.markJobPlayable(job)

	if !job.Available {
		t.Error("markJobPlayable should set Available = true")
	}
	if job.HLSUrl != "/hls/job1/master.m3u8" {
		t.Errorf("job.HLSUrl = %q, want %q", job.HLSUrl, "/hls/job1/master.m3u8")
	}
	if job.Status != models.HLSStatusRunning {
		t.Errorf("job.Status = %v, want unchanged Running — availability is reported independently of status (see applyHLSCompletionFields)", job.Status)
	}
}

// ---------------------------------------------------------------------------
// HasHLS / HasHLSByID / VariantDownloadPath: Available (not Status=="completed")
// is the consistent "is HLS ready" gate — see internal/hls package doc notes.
// ---------------------------------------------------------------------------

func TestHasHLS_ReportsTrueWhileRunningIfAvailable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, masterPlaylistName), []byte("#EXTM3U"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Module{
		jobs: map[string]*models.HLSJob{
			"job1": {ID: "job1", MediaPath: "/media/movie.mp4", OutputDir: dir, Status: models.HLSStatusRunning, Available: true},
		},
		log: logger.New("test"),
	}
	if !m.HasHLS("/media/movie.mp4") {
		t.Error("HasHLS should report true for a running-but-Available job with a master playlist on disk")
	}
}

func TestHasHLS_ReportsFalseWhileRunningIfNotYetAvailable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, masterPlaylistName), []byte("#EXTM3U"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Module{
		jobs: map[string]*models.HLSJob{
			"job1": {ID: "job1", MediaPath: "/media/movie.mp4", OutputDir: dir, Status: models.HLSStatusRunning},
		},
		log: logger.New("test"),
	}
	if m.HasHLS("/media/movie.mp4") {
		t.Error("HasHLS should report false before the first quality has published")
	}
}

func TestVariantDownloadPath_AvailableWhileRunning(t *testing.T) {
	dir := t.TempDir()
	qualityDir := filepath.Join(dir, "360p")
	if err := os.MkdirAll(qualityDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(qualityDir, "playlist.m3u8"), []byte("#EXTM3U"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Module{
		ffmpegPath: "/usr/bin/ffmpeg",
		jobs: map[string]*models.HLSJob{
			"job1": {ID: "job1", OutputDir: dir, Status: models.HLSStatusRunning, Available: true},
		},
		log: logger.New("test"),
	}

	path, err := m.VariantDownloadPath("job1", "360p")
	if err != nil {
		t.Fatalf("VariantDownloadPath: %v", err)
	}
	if want := filepath.Join(qualityDir, "playlist.m3u8"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

func TestVariantDownloadPath_NotYetAvailable_Errors(t *testing.T) {
	m := &Module{
		ffmpegPath: "/usr/bin/ffmpeg",
		jobs: map[string]*models.HLSJob{
			"job1": {ID: "job1", OutputDir: t.TempDir(), Status: models.HLSStatusRunning},
		},
		log: logger.New("test"),
	}
	if _, err := m.VariantDownloadPath("job1", "360p"); err == nil {
		t.Error("expected an error for a job that is not yet Available")
	}
}

// ---------------------------------------------------------------------------
// ServeMasterPlaylist / ServeSegment: serve a playable-but-running job, but
// never an in-progress variant.
// ---------------------------------------------------------------------------

func TestServeMasterPlaylist_AvailableWhileRunning_Serves(t *testing.T) {
	outputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outputDir, masterPlaylistName), []byte("#EXTM3U\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	job := &models.HLSJob{ID: "job1", OutputDir: outputDir, Status: models.HLSStatusRunning, Available: true}
	m := &Module{
		jobs:   map[string]*models.HLSJob{"job1": job},
		log:    logger.New("test"),
		config: config.NewManager(filepath.Join(t.TempDir(), "config.json")),
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/hls/job1/master.m3u8", nil)
	if err := m.ServeMasterPlaylist(w, r, "job1"); err != nil {
		t.Fatalf("ServeMasterPlaylist: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestServeMasterPlaylist_NotYetAvailable_ReturnsErrNotReady(t *testing.T) {
	job := &models.HLSJob{ID: "job1", OutputDir: t.TempDir(), Status: models.HLSStatusRunning}
	m := &Module{
		jobs:   map[string]*models.HLSJob{"job1": job},
		log:    logger.New("test"),
		config: config.NewManager(filepath.Join(t.TempDir(), "config.json")),
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/hls/job1/master.m3u8", nil)
	err := m.ServeMasterPlaylist(w, r, "job1")
	if !errors.Is(err, ErrNotReady) {
		t.Fatalf("err = %v, want ErrNotReady", err)
	}
}

func TestServeMasterPlaylist_CanceledButAvailable_StillServes(t *testing.T) {
	// A job canceled (e.g. server shutdown) after already becoming playable
	// must remain servable — see finalizeAfterQualityFailure.
	outputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outputDir, masterPlaylistName), []byte("#EXTM3U\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	job := &models.HLSJob{ID: "job1", OutputDir: outputDir, Status: models.HLSStatusCanceled, Available: true}
	m := &Module{
		jobs:   map[string]*models.HLSJob{"job1": job},
		log:    logger.New("test"),
		config: config.NewManager(filepath.Join(t.TempDir(), "config.json")),
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/hls/job1/master.m3u8", nil)
	if err := m.ServeMasterPlaylist(w, r, "job1"); err != nil {
		t.Fatalf("ServeMasterPlaylist: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestServeSegment_InProgressVariant_ReturnsErrNotReady(t *testing.T) {
	outputDir := t.TempDir()
	qualityDir := filepath.Join(outputDir, "720p")
	if err := os.MkdirAll(qualityDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Segment already on disk but playlist.m3u8 is not — simulates a quality
	// still mid-encode (buildFFmpegTranscodeCmd uses hls_playlist_type=vod, so
	// ffmpeg only writes playlist.m3u8 once the whole encode finishes).
	if err := os.WriteFile(filepath.Join(qualityDir, "segment_0000.ts"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	job := &models.HLSJob{ID: "job1", OutputDir: outputDir, Status: models.HLSStatusRunning, Available: true}
	m := &Module{
		jobs:   map[string]*models.HLSJob{"job1": job},
		log:    logger.New("test"),
		config: config.NewManager(filepath.Join(t.TempDir(), "config.json")),
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/hls/job1/720p/segment_0000.ts", nil)
	err := m.ServeSegment(w, r, SegmentParams{JobID: "job1", Quality: "720p", Segment: "segment_0000.ts"})
	if !errors.Is(err, ErrNotReady) {
		t.Fatalf("err = %v, want ErrNotReady — in-progress variant dirs must not be served", err)
	}
}

func TestServeSegment_CompletedVariant_Serves(t *testing.T) {
	outputDir := t.TempDir()
	qualityDir := filepath.Join(outputDir, "720p")
	if err := os.MkdirAll(qualityDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(qualityDir, "playlist.m3u8"), []byte("#EXTM3U\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(qualityDir, "segment_0000.ts"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	job := &models.HLSJob{ID: "job1", OutputDir: outputDir, Status: models.HLSStatusRunning, Available: true}
	m := &Module{
		jobs:   map[string]*models.HLSJob{"job1": job},
		log:    logger.New("test"),
		config: config.NewManager(filepath.Join(t.TempDir(), "config.json")),
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/hls/job1/720p/segment_0000.ts", nil)
	if err := m.ServeSegment(w, r, SegmentParams{JobID: "job1", Quality: "720p", Segment: "segment_0000.ts"}); err != nil {
		t.Fatalf("ServeSegment: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ---------------------------------------------------------------------------
// validateExistingHLS: accepts a partial ladder instead of requiring every
// originally-requested quality.
// ---------------------------------------------------------------------------

func TestValidateExistingHLS_AcceptsPartialLadder(t *testing.T) {
	outputDir := t.TempDir()
	qualityDir := filepath.Join(outputDir, "360p")
	if err := os.MkdirAll(qualityDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(qualityDir, "playlist.m3u8"), []byte("#EXTINF:6.0,\nseg_0000.ts\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(qualityDir, "seg_0000.ts"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Only 360p is listed — as transcode()'s progressive publishMasterPlaylist
	// would leave it if the job was interrupted right after this one quality.
	master := "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=500000\n360p/playlist.m3u8\n"
	if err := os.WriteFile(filepath.Join(outputDir, masterPlaylistName), []byte(master), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &Module{log: logger.New("test")}
	got := m.validateExistingHLS(&validateExistingHLSParams{OutputDir: outputDir})
	if !slices.Equal(got, []string{"360p"}) {
		t.Errorf("validateExistingHLS() = %v, want [360p] even though a full 2+ quality ladder was never finished", got)
	}
}

func TestValidateExistingHLS_NoMaster_ReturnsNil(t *testing.T) {
	m := &Module{log: logger.New("test")}
	got := m.validateExistingHLS(&validateExistingHLSParams{OutputDir: t.TempDir()})
	if got != nil {
		t.Errorf("validateExistingHLS() = %v, want nil", got)
	}
}

// ---------------------------------------------------------------------------
// discoverExistingJobs: accepts a partial-ladder master on restart, marks the
// discovered job Available, and removes a leftover in-progress variant dir
// that isn't referenced by master.m3u8.
// ---------------------------------------------------------------------------

func TestDiscoverExistingJobs_AcceptsPartialLadderAndRemovesLeftoverDir(t *testing.T) {
	cacheDir := t.TempDir()
	jobDir := filepath.Join(cacheDir, "job1")
	doneDir := filepath.Join(jobDir, "360p")
	if err := os.MkdirAll(doneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(doneDir, "playlist.m3u8"), []byte("#EXTINF:6.0,\nseg_0000.ts\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(doneDir, "seg_0000.ts"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	master := "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=500000\n360p/playlist.m3u8\n"
	if err := os.WriteFile(filepath.Join(jobDir, masterPlaylistName), []byte(master), 0o644); err != nil {
		t.Fatal(err)
	}
	// A second quality that was mid-encode when the server stopped: segments
	// exist but its own playlist.m3u8 was never written, and it is not listed
	// in master.m3u8 above. It must be removed, not discovered or advertised.
	inProgressDir := filepath.Join(jobDir, "720p")
	if err := os.MkdirAll(inProgressDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inProgressDir, "seg_0000.ts"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &Module{
		cacheDir: cacheDir,
		jobs:     map[string]*models.HLSJob{},
		log:      logger.New("test"),
	}
	if discovered := m.discoverExistingJobs(); discovered != 1 {
		t.Fatalf("discoverExistingJobs() = %d, want 1", discovered)
	}

	job, ok := m.jobs["job1"]
	if !ok {
		t.Fatal("expected job1 to be discovered")
	}
	if job.Status != models.HLSStatusCompleted {
		t.Errorf("job.Status = %v, want Completed", job.Status)
	}
	if !job.Available {
		t.Error("discovered job should be Available")
	}
	if job.HLSUrl != "/hls/job1/master.m3u8" {
		t.Errorf("job.HLSUrl = %q, want %q", job.HLSUrl, "/hls/job1/master.m3u8")
	}
	if !slices.Equal(job.Qualities, []string{"360p"}) {
		t.Errorf("job.Qualities = %v, want [360p]", job.Qualities)
	}
	if _, statErr := os.Stat(inProgressDir); !os.IsNotExist(statErr) {
		t.Error("leftover in-progress variant dir should have been removed")
	}
	if _, statErr := os.Stat(doneDir); statErr != nil {
		t.Errorf("completed variant dir should NOT have been touched: %v", statErr)
	}
}
