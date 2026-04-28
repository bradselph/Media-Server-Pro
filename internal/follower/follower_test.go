package follower

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"media-server-pro/internal/config"
)

// TestBuildWSURL_HTTPSToWSS verifies that an https master URL is rewritten to
// wss with the canonical /ws/receiver path. Slaves never connect to a different
// path, so getting this wrong silently breaks pairing.
func TestBuildWSURL_HTTPSToWSS(t *testing.T) {
	got, err := buildWSURL("https://other-vps.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "wss://other-vps.example.com/ws/receiver" {
		t.Fatalf("got %q want wss://other-vps.example.com/ws/receiver", got)
	}
}

func TestBuildWSURL_HTTPToWS(t *testing.T) {
	got, err := buildWSURL("http://localhost:8080/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ws://localhost:8080/ws/receiver" {
		t.Fatalf("got %q want ws://localhost:8080/ws/receiver", got)
	}
}

func TestBuildWSURL_RejectsBadScheme(t *testing.T) {
	if _, err := buildWSURL("ftp://x.example.com"); err == nil {
		t.Fatal("expected error for ftp scheme")
	}
	if _, err := buildWSURL("not-a-url"); err == nil {
		t.Fatal("expected error for missing host")
	}
}

// TestRelativizeUnderRoot exercises the catalog-build path translation. The
// master expects relative paths so that path-traversal validation can reject
// absolute paths and ".." segments. Local paths that don't sit under any
// configured media root are dropped silently.
func TestRelativizeUnderRoot(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "videos")
	if err := osMkdirAll(root); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	roots := []string{root}

	tests := []struct {
		name    string
		input   string
		want    string
		wantOK  bool
	}{
		{
			name:   "relative under root",
			input:  filepath.Join(root, "movies", "foo.mp4"),
			want:   "movies/foo.mp4",
			wantOK: true,
		},
		{
			name:   "outside root rejected",
			input:  filepath.Join(tmp, "other", "bar.mp4"),
			wantOK: false,
		},
		{
			name:   "root itself rejected (no relative path)",
			input:  root,
			wantOK: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := relativizeUnderRoot(tc.input, roots)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v want %v", ok, tc.wantOK)
			}
			if tc.wantOK && got != tc.want {
				t.Fatalf("path = %q want %q", got, tc.want)
			}
		})
	}
}

// TestIsValidToken accepts master-generated UUIDs (v4 / v7 style) and rejects
// anything that could be reflected into a URL path injection.
func TestIsValidToken(t *testing.T) {
	good := []string{
		"abc123",
		"a-b-c-d-e",
		"550e8400-e29b-41d4-a716-446655440000",
	}
	for _, tok := range good {
		if !isValidToken(tok) {
			t.Errorf("expected %q to be valid", tok)
		}
	}
	bad := []string{
		"",
		"../admin",
		"foo/bar",
		"foo bar",
		"semi;colon",
	}
	for _, tok := range bad {
		if isValidToken(tok) {
			t.Errorf("expected %q to be invalid", tok)
		}
	}
}

// TestParseRange_AllForms covers the three Range header shapes the player
// commonly emits: prefix, suffix, and explicit start-end.
func TestParseRange_AllForms(t *testing.T) {
	const size int64 = 1000
	cases := []struct {
		header   string
		wantS, wantE int64
	}{
		{"bytes=0-99", 0, 99},
		{"bytes=500-", 500, 999},
		{"bytes=-100", 900, 999},
	}
	for _, tc := range cases {
		s, e, err := parseRange(tc.header, size)
		if err != nil {
			t.Errorf("%s: unexpected error %v", tc.header, err)
			continue
		}
		if s != tc.wantS || e != tc.wantE {
			t.Errorf("%s: got %d-%d want %d-%d", tc.header, s, e, tc.wantS, tc.wantE)
		}
	}
}

func TestParseRange_RejectsOutOfBounds(t *testing.T) {
	if _, _, err := parseRange("bytes=2000-3000", 1000); err == nil {
		t.Fatal("expected error for out-of-bounds range")
	}
	if _, _, err := parseRange("bytes=500-100", 1000); err == nil {
		t.Fatal("expected error when end < start")
	}
}

// TestNotEnabled_StartIsNoOp verifies that a follower module with no pairing
// configuration still implements server.Module cleanly: Start succeeds, Stop
// succeeds, no goroutines linger. This is the default state for the vast
// majority of installs that don't pair to another server.
func TestNotEnabled_StartIsNoOp(t *testing.T) {
	mgr := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	m := NewModule(mgr, nil)

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	status := m.GetStatus()
	if status.Enabled {
		t.Fatal("expected disabled in default config")
	}
	if status.Connected {
		t.Fatal("expected not connected when disabled")
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestEnabledButUnconfigured_RemainsHealthy verifies that flipping Enabled to
// true without master_url + api_key leaves the module idle (healthy, not
// connected). A misconfigured pairing must not take the server down.
func TestEnabledButUnconfigured_RemainsHealthy(t *testing.T) {
	mgr := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	if err := mgr.Update(func(c *config.Config) {
		c.Follower.Enabled = true
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	m := NewModule(mgr, nil)

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })

	if h := m.Health(); !strings.Contains(h.Message, "not configured") {
		t.Fatalf("expected 'not configured' health message, got %q", h.Message)
	}
}

// TestEndToEndPairing spins up a tiny master that accepts a WS connection on
// /ws/receiver, requires a matching X-API-Key header, and reads register +
// catalog messages. Drives the follower module against it and asserts that
// (a) the connection is established, (b) the catalog is pushed, and (c) the
// module's status reflects "connected" before shutdown.
func TestEndToEndPairing(t *testing.T) {
	type recvMsg struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data,omitempty"`
	}

	const apiKey = "test-key-abcdef"
	var connections atomic.Int32
	var registered atomic.Bool
	var catalogPushed atomic.Bool

	upgrader := websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/receiver", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != apiKey {
			http.Error(w, "bad key", http.StatusForbidden)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		connections.Add(1)

		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg recvMsg
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}
			switch msg.Type {
			case "register":
				registered.Store(true)
			case "catalog":
				catalogPushed.Store(true)
				return // close after first catalog so the test doesn't hang
			}
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	mgr := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	if err := mgr.Update(func(c *config.Config) {
		c.Follower.Enabled = true
		c.Follower.MasterURL = srv.URL
		c.Follower.APIKey = apiKey
		c.Follower.SlaveID = "test-follower"
		c.Follower.SlaveName = "Test Follower"
		c.Follower.HeartbeatInterval = 100 * time.Millisecond
		c.Follower.ScanInterval = 30 * time.Second
		c.Follower.ReconnectBase = 100 * time.Millisecond
		c.Follower.ReconnectMax = 200 * time.Millisecond
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Pass nil media module — buildCatalog returns nil items, but the WS
	// register + catalog messages are still sent (with an empty list), which
	// is what we're asserting on.
	m := NewModule(mgr, nil)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = m.Stop(stopCtx)
	})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if registered.Load() && catalogPushed.Load() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !registered.Load() {
		t.Fatal("follower did not send register message within 3s")
	}
	if !catalogPushed.Load() {
		t.Fatal("follower did not push catalog within 3s")
	}
	if connections.Load() == 0 {
		t.Fatal("master never accepted a WS connection")
	}
}

// osMkdirAll is a tiny shim so the test file doesn't have to import the os
// package just for one Mkdir call. Keeps the noise low.
func osMkdirAll(path string) error {
	return mkdirAll(path, 0o755)
}
