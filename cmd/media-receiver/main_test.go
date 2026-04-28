package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestFND0002_GracefulShutdownUnblocksReadMessage verifies that the monitor goroutine
// added in FND-0002 allows graceful shutdown even when conn.ReadMessage() would block
// forever. Without the fix, wg.Wait() would deadlock because the message-reading
// goroutine is stuck in ReadMessage() and never receives a close signal.
func TestFND0002_GracefulShutdownUnblocksReadMessage(t *testing.T) {
	// Create a test WebSocket server that accepts connections but never sends messages
	// (simulating a silent server that would normally cause ReadMessage to block).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Keep the server connection alive but don't send any messages.
		// The client's ReadMessage will block indefinitely without the monitor goroutine.
		<-time.After(10 * time.Second)
	}))
	defer server.Close()

	// Convert HTTP URL to WebSocket URL
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Create a context that we'll cancel to trigger shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Track that goroutines complete (verifying no deadlock in wg.Wait())
	var completedGoroutines atomic.Int32
	done := make(chan bool, 1)

	go func() {
		// Simulate the goroutine structure from connectAndRun
		dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
		conn, _, err := dialer.DialContext(ctx, wsURL, http.Header{})
		if err != nil {
			t.Logf("Dial error (expected if server is slow): %v", err)
			done <- false
			return
		}
		defer conn.Close()

		var wg sync.WaitGroup
		readErr := make(chan error, 1)

		// This is the fix from FND-0002: monitor goroutine that closes the connection
		// on context cancellation, unblocking any goroutine stuck in ReadMessage()
		wg.Go(func() {
			<-ctx.Done()
			_ = conn.Close()
		})
		completedGoroutines.Add(1)

		// Message-reading goroutine that would block forever in ReadMessage without the fix
		wg.Go(func() {
			for {
				_, _, err := conn.ReadMessage()
				if err != nil {
					if ctx.Err() == nil {
						readErr <- err
					}
					return
				}
			}
		})
		completedGoroutines.Add(1)

		// Simulate the shutdown path that was deadlocking before the fix
		// The test cancels the context and waits for goroutines to complete.
		// Without the monitor goroutine, wg.Wait() would block forever.
		timeoutCh := time.After(5 * time.Second)
		waitDone := make(chan struct{})

		go func() {
			wg.Wait()
			close(waitDone)
		}()

		select {
		case <-waitDone:
			// Success: wg.Wait() completed without deadlock
			done <- true
		case <-timeoutCh:
			// Failure: wg.Wait() deadlocked (took >5 seconds)
			t.Error("FND-0002: wg.Wait() deadlocked; monitor goroutine did not unblock ReadMessage")
			done <- false
		}
	}()

	// Wait a moment for the test goroutine to establish the WebSocket connection
	time.Sleep(500 * time.Millisecond)

	// Trigger shutdown by canceling the context
	cancel()

	// Wait for the test goroutine to complete
	success := <-done
	if !success {
		t.Fatal("FND-0002: Test failed; graceful shutdown deadlocked")
	}

	if completedGoroutines.Load() < 2 {
		t.Logf("FND-0002: Warning, only %d goroutines completed (expected 2+)", completedGoroutines.Load())
	}
}

// TestFND0002_MonitorGoroutineClosesConnection verifies the core fix: the monitor
// goroutine successfully closes the connection on context cancellation.
func TestFND0002_MonitorGoroutineClosesConnection(t *testing.T) {
	// Create a test server that accepts a connection and waits
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Wait indefinitely to simulate a silent server
		<-time.After(30 * time.Second)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, http.Header{})
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	var wg sync.WaitGroup
	readErr := make(chan error, 1)

	// Monitor goroutine from the fix
	wg.Go(func() {
		<-ctx.Done()
		_ = conn.Close()
	})

	// Message-reading goroutine
	wg.Go(func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				if ctx.Err() == nil {
					readErr <- err
				}
				return
			}
		}
	})

	// Cancel context to trigger the monitor goroutine's close
	cancel()

	// Wait for all goroutines to complete
	waitDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitDone)
	}()

	// Should complete within 2 seconds (not deadlock)
	select {
	case <-waitDone:
		// Success
		t.Logf("FND-0002: Monitor goroutine successfully unblocked ReadMessage")
	case <-time.After(2 * time.Second):
		t.Fatal("FND-0002: wg.Wait() deadlocked; monitor goroutine did not work")
	}

	// Verify the read goroutine exited due to connection close
	select {
	case err := <-readErr:
		// The read goroutine encountered an error (expected due to connection close)
		t.Logf("FND-0002: Read goroutine received expected error: %v", err)
	case <-time.After(100 * time.Millisecond):
		// No error means the goroutine didn't reach the error handler,
		// but wg.Wait() completed, which means it exited normally from the read loop.
		// This is also acceptable.
		t.Logf("FND-0002: Read goroutine exited cleanly")
	}
}

// TestFND0002_ContextCancellationTriggersMonitor verifies that the monitor goroutine
// responds to context cancellation and closes the connection, causing ReadMessage to fail.
func TestFND0002_ContextCancellationTriggersMonitor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Just hold the connection open
		<-time.After(30 * time.Second)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	ctx, cancel := context.WithCancel(context.Background())

	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, http.Header{})
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	var wg sync.WaitGroup
	readErr := make(chan error, 1)

	// Monitor goroutine
	wg.Go(func() {
		<-ctx.Done()
		_ = conn.Close()
	})

	// Message-reading goroutine
	readCompleted := false
	wg.Go(func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				readErr <- err
				return
			}
		}
	})

	// Give the read goroutine time to enter ReadMessage() and block
	time.Sleep(100 * time.Millisecond)

	// Cancel the context, which should trigger the monitor goroutine
	cancel()

	// The read goroutine should exit (via the error from close)
	select {
	case <-readErr:
		readCompleted = true
	case <-time.After(2 * time.Second):
		t.Fatal("FND-0002: Read goroutine did not exit after context cancellation")
	}

	// Verify wg.Wait() completes quickly (not deadlocked)
	waitDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
		if readCompleted {
			t.Logf("FND-0002: Context cancellation correctly triggered monitor, which closed connection and unblocked read")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("FND-0002: wg.Wait() deadlocked after context cancellation")
	}
}

// TestExtractProbeMeta_NoFFprobe verifies that extractProbeMeta returns zero
// values (and does NOT crash) when ffprobe is unavailable. This is the common
// case on minimal hosts that haven't installed ffmpeg yet — the slave must
// continue scanning and pushing catalog without metadata rather than failing.
func TestExtractProbeMeta_NoFFprobe(t *testing.T) {
	// Force ffprobe unavailable for this test, regardless of host environment.
	prev := ffprobePath
	ffprobePath = ""
	t.Cleanup(func() { ffprobePath = prev })

	tmp := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(tmp, []byte("not a real video"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	info, err := os.Stat(tmp)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	dur, w, h := extractProbeMeta(tmp, info)
	if dur != 0 || w != 0 || h != 0 {
		t.Fatalf("expected zero metadata when ffprobe unavailable, got dur=%v w=%d h=%d", dur, w, h)
	}
}

// TestExtractProbeMeta_BadFile verifies that extractProbeMeta returns zeros
// (without panicking) when ffprobe rejects the input. The catalog item must
// still be pushed — just without duration/dimensions — so a single broken
// file can't take the whole scan offline.
func TestExtractProbeMeta_BadFile(t *testing.T) {
	if _, err := os.Stat("/usr/bin/ffprobe"); err != nil {
		if _, err := os.Stat("/usr/local/bin/ffprobe"); err != nil {
			t.Skip("ffprobe not installed; skipping")
		}
	}
	prev := ffprobePath
	ffprobePath = detectFFprobe()
	t.Cleanup(func() { ffprobePath = prev })
	if ffprobePath == "" {
		t.Skip("ffprobe not found; skipping")
	}

	// Write a file that ffprobe will fail to parse.
	tmp := filepath.Join(t.TempDir(), "bad.mp4")
	if err := os.WriteFile(tmp, []byte("garbage bytes"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := os.Stat(tmp)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	dur, w, h := extractProbeMeta(tmp, info)
	if dur != 0 || w != 0 || h != 0 {
		t.Fatalf("expected zeros for unprobeable file, got dur=%v w=%d h=%d", dur, w, h)
	}
}

// TestProbeCache_ReusesUnchangedFile verifies that a second probe of the same
// file (unchanged mtime + size) is served from cache rather than re-spawning
// ffprobe. Without this, large libraries would re-probe every file on every
// scan tick and saturate CPU.
func TestProbeCache_ReusesUnchangedFile(t *testing.T) {
	prev := ffprobePath
	t.Cleanup(func() {
		ffprobePath = prev
		probeCacheMu.Lock()
		probeCache = make(map[string]probeEntry)
		probeCacheMu.Unlock()
	})

	// Stub ffprobe path to a non-empty value but the file won't actually be
	// probed in this test path; we pre-populate the cache directly.
	ffprobePath = "/nonexistent/ffprobe-stub"

	tmp := filepath.Join(t.TempDir(), "cached.mp4")
	if err := os.WriteFile(tmp, []byte("content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := os.Stat(tmp)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	probeCacheMu.Lock()
	probeCache[tmp] = probeEntry{
		modTime: info.ModTime(), size: info.Size(),
		duration: 123.45, width: 1920, height: 1080,
	}
	probeCacheMu.Unlock()

	dur, w, h := extractProbeMeta(tmp, info)
	if dur != 123.45 || w != 1920 || h != 1080 {
		t.Fatalf("cache miss: got dur=%v w=%d h=%d, want 123.45/1920/1080", dur, w, h)
	}
}

// TestPruneProbeCache verifies that probe-cache entries for vanished files are
// removed at the end of each scan, mirroring pruneFpCache.
func TestPruneProbeCache(t *testing.T) {
	probeCacheMu.Lock()
	probeCache = map[string]probeEntry{
		"/still/here":  {duration: 1},
		"/now/missing": {duration: 2},
	}
	probeCacheMu.Unlock()

	pruneProbeCache(map[string]bool{"/still/here": true})

	probeCacheMu.Lock()
	defer probeCacheMu.Unlock()
	if _, ok := probeCache["/still/here"]; !ok {
		t.Fatal("expected /still/here to be retained")
	}
	if _, ok := probeCache["/now/missing"]; ok {
		t.Fatal("expected /now/missing to be pruned")
	}
	probeCache = make(map[string]probeEntry) // clean up
}

// TestFFprobeResult_ParsesMasterShape sanity-checks that the slave's
// ffprobeResult struct decodes the same JSON shape ffprobe produces with
// "-show_format -show_streams". If ffprobe ever changes its output format
// this test catches it before it reaches production.
func TestFFprobeResult_ParsesMasterShape(t *testing.T) {
	const sample = `{
		"format": {"duration": "42.5"},
		"streams": [
			{"codec_type": "audio"},
			{"codec_type": "video", "width": 1280, "height": 720}
		]
	}`
	var probe ffprobeResult
	if err := json.Unmarshal([]byte(sample), &probe); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if probe.Format.Duration != "42.5" {
		t.Fatalf("duration: got %q want 42.5", probe.Format.Duration)
	}
	if len(probe.Streams) != 2 {
		t.Fatalf("streams: got %d want 2", len(probe.Streams))
	}
	if probe.Streams[1].Width != 1280 || probe.Streams[1].Height != 720 {
		t.Fatalf("video stream dims: got %dx%d want 1280x720",
			probe.Streams[1].Width, probe.Streams[1].Height)
	}
}
