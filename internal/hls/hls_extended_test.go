package hls

import (
	"testing"
	"time"

	"media-server-pro/internal/logger"
	"media-server-pro/pkg/models"
)

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
	if !containsStr(got, "#EXTM3U") {
		t.Error("should preserve #EXTM3U tag")
	}
	// Segment lines should be rewritten with base URL
	if !containsStr(got, "/hls/job123/720p/seg001.ts") {
		t.Errorf("should rewrite segment URI, got:\n%s", got)
	}
	if !containsStr(got, "/hls/job123/720p/seg002.ts") {
		t.Errorf("should rewrite second segment URI, got:\n%s", got)
	}
}

func TestRewritePlaylistLines_PreservesComments(t *testing.T) {
	data := []byte("#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:10.0,\nseg.ts\n#EXT-X-ENDLIST\n")
	got := string(rewritePlaylistLines(data, "/base/"))
	if !containsStr(got, "#EXT-X-VERSION:3") {
		t.Error("should preserve version tag")
	}
	if !containsStr(got, "#EXT-X-ENDLIST") {
		t.Error("should preserve endlist tag")
	}
}

func TestRewritePlaylistLines_EmptyInput(t *testing.T) {
	got := rewritePlaylistLines(nil, "/base/")
	if len(got) == 0 {
		// nil input may produce empty or single newline, both OK
	}
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
	now := time.Now()
	original := &models.HLSJob{
		ID:        "job-1",
		MediaPath: "/test/media-1.mp4",
		Status:    models.HLSStatusCompleted,
		Qualities: []string{"720p", "1080p"},
		CompletedAt: &now,
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

// helper
func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
