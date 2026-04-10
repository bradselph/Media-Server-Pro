package streaming

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// isMobileDevice
// ---------------------------------------------------------------------------

func TestIsMobileDevice_Mobile(t *testing.T) {
	m := newTestModule(t)
	mobileUAs := []string{
		"Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X)",
		"Mozilla/5.0 (Linux; Android 13; Pixel 7)",
		"Mozilla/5.0 (iPad; CPU OS 16_0 like Mac OS X)",
		"BlackBerry/10.0",
		"Mozilla/5.0 (compatible; MSIE 10.0; Windows Phone 8.0)",
		"Opera Mini/8.0",
		"Opera Mobi/12.10",
	}
	for _, ua := range mobileUAs {
		if !m.isMobileDevice(ua) {
			t.Errorf("isMobileDevice(%q) = false, want true", ua)
		}
	}
}

func TestIsMobileDevice_Desktop(t *testing.T) {
	m := newTestModule(t)
	desktopUAs := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 13_0)",
		"Mozilla/5.0 (X11; Linux x86_64)",
		"curl/7.88.1",
		"",
	}
	for _, ua := range desktopUAs {
		if m.isMobileDevice(ua) {
			t.Errorf("isMobileDevice(%q) = true, want false", ua)
		}
	}
}

func TestIsMobileDevice_CaseInsensitive(t *testing.T) {
	m := newTestModule(t)
	if !m.isMobileDevice("MOZILLA/5.0 (IPHONE)") {
		t.Error("isMobileDevice should be case-insensitive")
	}
}

// ---------------------------------------------------------------------------
// getContentType — additional extensions
// ---------------------------------------------------------------------------

func TestGetContentType_Audio(t *testing.T) {
	m := newTestModule(t)
	tests := []struct {
		path string
		want string
	}{
		{"song.mp3", "audio/mpeg"},
		{"track.flac", "audio/flac"},
		{"sound.ogg", "audio/ogg"},
	}
	for _, tc := range tests {
		got := m.getContentType(tc.path)
		if got != tc.want {
			t.Errorf("getContentType(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// parseRange — additional cases
// ---------------------------------------------------------------------------

func TestParseRange_Full(t *testing.T) {
	m := newTestModule(t)
	start, end, err := m.parseRange("bytes=0-99", 100)
	if err != nil {
		t.Fatalf("parseRange: %v", err)
	}
	if start != 0 || end != 99 {
		t.Errorf("parseRange(0-99) = %d-%d, want 0-99", start, end)
	}
}

func TestParseRange_SuffixRange(t *testing.T) {
	m := newTestModule(t)
	start, end, err := m.parseRange("bytes=-50", 200)
	if err != nil {
		t.Fatalf("parseRange: %v", err)
	}
	if start != 150 || end != 199 {
		t.Errorf("parseRange(-50/200) = %d-%d, want 150-199", start, end)
	}
}

func TestParseRange_Invalid(t *testing.T) {
	m := newTestModule(t)
	_, _, err := m.parseRange("not-a-range", 100)
	if err == nil {
		t.Error("expected error for invalid range")
	}
}

func TestParseRange_OutOfBounds(t *testing.T) {
	m := newTestModule(t)
	_, _, err := m.parseRange("bytes=200-300", 100)
	if err == nil {
		t.Error("expected error for out-of-bounds range")
	}
}

// ---------------------------------------------------------------------------
// generateSessionID
// ---------------------------------------------------------------------------

func TestGenerateSessionID_Unique(t *testing.T) {
	ids := make(map[string]bool)
	for range 100 {
		id := generateSessionID("stream")
		if id == "" {
			t.Fatal("generateSessionID returned empty")
		}
		if ids[id] {
			t.Fatalf("duplicate session ID: %s", id)
		}
		ids[id] = true
	}
}

func TestGenerateSessionID_HasPrefix(t *testing.T) {
	id := generateSessionID("test")
	if len(id) < 5 {
		t.Error("session ID should not be empty")
	}
}

// ---------------------------------------------------------------------------
// session lifecycle — additional
// ---------------------------------------------------------------------------

func TestSessionLifecycle_MultipleStreams(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())

	s1 := m.startSession(StreamRequest{Path: "/video1.mp4", UserID: "user1"}, 0)
	s2 := m.startSession(StreamRequest{Path: "/video2.mp4", UserID: "user2"}, 0)

	if m.GetActiveStreamCount("user1") != 1 {
		t.Errorf("user1 active streams = %d, want 1", m.GetActiveStreamCount("user1"))
	}

	m.endSession(s1.ID)
	if m.GetActiveStreamCount("user1") != 0 {
		t.Errorf("after ending user1, active = %d, want 0", m.GetActiveStreamCount("user1"))
	}

	m.endSession(s2.ID)
	if m.GetActiveStreamCount("user2") != 0 {
		t.Errorf("after ending all, active = %d, want 0", m.GetActiveStreamCount("user2"))
	}
}

func TestEndSession_NonExistent(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())
	m.endSession("non-existent-id")
}

// ---------------------------------------------------------------------------
// GetStats
// ---------------------------------------------------------------------------

func TestGetStats_Empty(t *testing.T) {
	m := newTestModule(t)
	m.Start(context.Background())
	defer m.Stop(context.Background())
	stats := m.GetStats()
	if stats.ActiveStreams != 0 {
		t.Errorf("ActiveStreams = %d, want 0", stats.ActiveStreams)
	}
}
