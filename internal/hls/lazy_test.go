package hls

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/pkg/models"
)

// ---------------------------------------------------------------------------
// H4: Lazy Transcode's on-demand path made reachable (C01) and non-blocking
// (X01), plus C23's empty-quality-ladder rejection.
//
// newTestModuleWithConfig (first_quality_test.go) supplies a real config
// manager seeded with DefaultConfig()'s quality profiles (1080p/720p/480p/
// 360p, all enabled), which these tests rely on for generateMasterPlaylist's
// getQualityProfile lookups.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// lazyMasterVariants: completed-first ordering (hls.js's LevelController uses
// the first #EXT-X-STREAM-INF entry as the default start level).
// ---------------------------------------------------------------------------

func TestLazyMasterVariants_CompletedFirstThenRestOfLadder(t *testing.T) {
	job := &models.HLSJob{Qualities: []string{"1080p", "720p", "480p", "360p"}}
	got := lazyMasterVariants(job, []string{"480p"})
	want := []string{"480p", "1080p", "720p", "360p"}
	if !slices.Equal(got, want) {
		t.Errorf("lazyMasterVariants(%v, %v) = %v, want %v", job.Qualities, []string{"480p"}, got, want)
	}
}

func TestLazyMasterVariants_MultipleCompletedPreserveFinishOrder(t *testing.T) {
	job := &models.HLSJob{Qualities: []string{"1080p", "720p", "480p", "360p"}}
	got := lazyMasterVariants(job, []string{"480p", "1080p"})
	want := []string{"480p", "1080p", "720p", "360p"}
	if !slices.Equal(got, want) {
		t.Errorf("lazyMasterVariants = %v, want %v", got, want)
	}
}

func TestLazyMasterVariants_NothingCompletedYet(t *testing.T) {
	job := &models.HLSJob{Qualities: []string{"720p", "360p"}}
	got := lazyMasterVariants(job, nil)
	want := []string{"720p", "360p"}
	if !slices.Equal(got, want) {
		t.Errorf("lazyMasterVariants = %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// publishLazyLadder: the extra republish that advertises the full ladder once
// lazy mode's eager quality has finished (C01).
// ---------------------------------------------------------------------------

func TestPublishLazyLadder_AdvertisesFullLadderWithEagerQualityFirst(t *testing.T) {
	m := newTestModuleWithConfig(t)
	outputDir := t.TempDir()
	job := &models.HLSJob{ID: "job1", OutputDir: outputDir, Qualities: []string{"1080p", "720p", "480p", "360p"}}

	// Only 720p was eagerly transcoded (lazy mode caps the eager loop to the
	// first quality — see resolveQualitiesToTranscode).
	m.publishLazyLadder(job, []string{"720p"})

	data, err := os.ReadFile(filepath.Join(outputDir, masterPlaylistName))
	if err != nil {
		t.Fatalf("read master: %v", err)
	}
	variants := m.parseVariantStreams(string(data))
	wantOrder := []string{"720p/playlist.m3u8", "1080p/playlist.m3u8", "480p/playlist.m3u8", "360p/playlist.m3u8"}
	if !slices.Equal(variants, wantOrder) {
		t.Errorf("master variants = %v, want %v (already-transcoded quality listed first, rest of ladder after)", variants, wantOrder)
	}
}

// ---------------------------------------------------------------------------
// ensureVariantPlaylistExists / triggerLazyTranscode: non-blocking dispatch
// (X01) with exactly-once dedup, using Module.lazyEncode to avoid invoking
// real ffmpeg.
// ---------------------------------------------------------------------------

func newLazyTestModule(t *testing.T) *Module {
	t.Helper()
	m := newTestModuleWithConfig(t)
	if err := m.config.Update(func(c *config.Config) { c.HLS.LazyTranscode = true }); err != nil {
		t.Fatalf("Update: %v", err)
	}
	return m
}

func TestEnsureVariantPlaylistExists_LazyMissing_ReturnsFastAndDispatchesExactlyOnce(t *testing.T) {
	m := newLazyTestModule(t)
	outputDir := t.TempDir()
	job := &models.HLSJob{ID: "job1", OutputDir: outputDir, Status: models.HLSStatusRunning, Qualities: []string{"720p", "360p"}}

	var calls int32
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	m.lazyEncode = func(_ context.Context, job *models.HLSJob, quality string) error {
		atomic.AddInt32(&calls, 1)
		started <- struct{}{}
		<-release // held open until the test lets the "encode" finish
		variantDir := filepath.Join(job.OutputDir, quality)
		if err := os.MkdirAll(variantDir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(variantDir, "playlist.m3u8"), []byte("#EXTM3U\n#EXT-X-ENDLIST\n"), 0o644)
	}

	start := time.Now()
	if _, err := m.ensureVariantPlaylistExists(job, "720p"); !errors.Is(err, ErrNotReady) {
		t.Fatalf("err = %v, want ErrNotReady", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("ensureVariantPlaylistExists took %v — it must return immediately instead of blocking for the encode (X01)", elapsed)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("background lazy transcode never started")
	}

	// A second, concurrent request for the SAME quality must join the
	// in-flight encode rather than starting a duplicate one.
	if _, err := m.ensureVariantPlaylistExists(job, "720p"); !errors.Is(err, ErrNotReady) {
		t.Fatalf("second call err = %v, want ErrNotReady", err)
	}

	close(release) // let the background "encode" finish

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(outputDir, "720p", "playlist.m3u8")); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("background encode invoked %d times, want exactly 1 (deduped via triggerLazyTranscode)", got)
	}

	path, err := m.ensureVariantPlaylistExists(job, "720p")
	if err != nil {
		t.Fatalf("ensureVariantPlaylistExists after background encode completed: %v", err)
	}
	if want := filepath.Join(outputDir, "720p", "playlist.m3u8"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

func TestEnsureVariantPlaylistExists_NonLazyMissingQuality_ReturnsErrNotReadyWithoutDispatch(t *testing.T) {
	m := newTestModuleWithConfig(t) // LazyTranscode left at its default (false)
	outputDir := t.TempDir()
	job := &models.HLSJob{ID: "job1", OutputDir: outputDir, Status: models.HLSStatusRunning}

	var calls int32
	m.lazyEncode = func(context.Context, *models.HLSJob, string) error {
		atomic.AddInt32(&calls, 1)
		return nil
	}

	if _, err := m.ensureVariantPlaylistExists(job, "720p"); !errors.Is(err, ErrNotReady) {
		t.Fatalf("err = %v, want ErrNotReady", err)
	}
	// Give any (incorrectly) dispatched goroutine a chance to run before asserting.
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Errorf("background encode invoked %d times, want 0 — lazy transcode is off", got)
	}
}

// ---------------------------------------------------------------------------
// triggerLazyTranscode / DeleteJob: the real dispatch path (not a manually
// simulated WaitGroup/cancel, unlike the existing TestDeleteJob_* in
// hls_extended_test.go) still gets drained and canceled by DeleteJob.
// ---------------------------------------------------------------------------

func TestDeleteJob_CancelsRealBackgroundLazyTranscode(t *testing.T) {
	outputDir := t.TempDir()
	m := newLazyTestModule(t)
	job := &models.HLSJob{ID: "job1", OutputDir: outputDir, Status: models.HLSStatusCompleted, Qualities: []string{"720p"}}
	m.repo = &stubHLSRepo{}
	m.jobs = map[string]*models.HLSJob{"job1": job}
	m.jobCancels = map[string]context.CancelFunc{}
	m.jobDone = map[string]chan struct{}{}
	m.accessTracker = &AccessTracker{lastAccess: make(map[string]time.Time), lastSaved: make(map[string]time.Time)}

	started := make(chan struct{})
	m.lazyEncode = func(ctx context.Context, _ *models.HLSJob, _ string) error {
		close(started)
		<-ctx.Done() // only returns once DeleteJob cancels this dispatch
		return ctx.Err()
	}

	m.triggerLazyTranscode(job, "720p")
	<-started

	done := make(chan error, 1)
	go func() { done <- m.DeleteJob("job1") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("DeleteJob: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("DeleteJob hung — cancellation of the background lazy transcode did not unblock the drain")
	}
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Errorf("output dir should have been removed by DeleteJob, stat err=%v", err)
	}
}

func TestTriggerLazyTranscode_ConcurrentDispatchDedupesToOneEncode(t *testing.T) {
	m := newLazyTestModule(t)
	job := &models.HLSJob{ID: "job1", OutputDir: t.TempDir(), Qualities: []string{"720p"}}

	var calls int32
	release := make(chan struct{})
	m.lazyEncode = func(context.Context, *models.HLSJob, string) error {
		atomic.AddInt32(&calls, 1)
		<-release
		return nil
	}

	const n = 10
	for range n {
		m.triggerLazyTranscode(job, "720p")
	}
	close(release)

	rawWg, ok := m.lazyWg.Load(job.ID)
	if !ok {
		t.Fatal("expected lazyWg to be registered")
	}
	waitDone := make(chan struct{})
	go func() {
		rawWg.(*sync.WaitGroup).Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("lazy dispatch(es) never finished")
	}

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("triggerLazyTranscode called the encode %d times across %d concurrent dispatches, want exactly 1", got, n)
	}
}

// ---------------------------------------------------------------------------
// C23: GenerateHLS rejects an empty resolved quality ladder before touching
// job state.
// ---------------------------------------------------------------------------

func TestGenerateHLS_NoEnabledQualityProfiles_ErrorsBeforeCreatingJob(t *testing.T) {
	// The companion internal/config/validate.go change (C23) now rejects any
	// Update()/SetValuesBatch() call that would leave hls.enabled=true with
	// zero enabled quality profiles, so that state can no longer be reached
	// through the normal config API. It can still exist in memory from an
	// on-disk config.json written before that validation existed: Load()
	// merges the file into m.config and surfaces the resulting validation
	// error, but — unlike Update()/SetValuesBatch — does not roll the merge
	// back on failure. Use that to build a Manager in this state and confirm
	// GenerateHLS's own defensive check (independent of the config-layer one)
	// still catches it.
	configPath := filepath.Join(t.TempDir(), "config.json")
	// quality_profiles_migrated must be true, otherwise migrateHLSQualityEnabled
	// treats an all-disabled ladder as a pre-Enabled-field legacy config and
	// force-enables every profile — defeating the scenario this test needs.
	raw := `{"hls":{"enabled":true,"quality_profiles_migrated":true,"quality_profiles":[{"name":"720p","width":1280,"height":720,"bitrate":2500000,"audio_bitrate":128000,"enabled":false}]}}`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := config.NewManager(configPath)
	if err := mgr.Load(); err == nil {
		t.Fatal("expected Load() to surface the zero-enabled-profiles validation error")
	}

	m := &Module{
		config:     mgr,
		log:        logger.New("test"),
		ffmpegPath: "/usr/bin/ffmpeg",
		jobs:       map[string]*models.HLSJob{},
		jobCancels: map[string]context.CancelFunc{},
		jobDone:    map[string]chan struct{}{},
		cacheDir:   t.TempDir(),
	}

	// Relative path so checkGenerateHLSPrereqs' os.Stat guard is skipped.
	job, err := m.GenerateHLS(context.Background(), &GenerateHLSParams{MediaPath: "video.mp4", MediaID: "job1"})
	if err == nil {
		t.Fatal("expected an error when no quality profiles are enabled")
	}
	if job != nil {
		t.Errorf("expected nil job on error, got %+v", job)
	}
	if _, exists := m.jobs["job1"]; exists {
		t.Error("GenerateHLS must not create a job when no qualities resolve (C23: fail before touching job state)")
	}
}
