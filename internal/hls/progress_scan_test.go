package hls

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"testing"

	"media-server-pro/internal/logger"
	"media-server-pro/pkg/models"
)

// ---------------------------------------------------------------------------
// scanFFmpegLines (C13: ffmpeg refreshes progress with a bare '\r')
// ---------------------------------------------------------------------------

func TestScanFFmpegLines_BareCR(t *testing.T) {
	advance, token, err := scanFFmpegLines([]byte("abc\rdef"), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if advance != 4 || string(token) != "abc" {
		t.Errorf("advance=%d token=%q, want advance=4 token=\"abc\"", advance, token)
	}
}

func TestScanFFmpegLines_LF(t *testing.T) {
	advance, token, err := scanFFmpegLines([]byte("abc\ndef"), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if advance != 4 || string(token) != "abc" {
		t.Errorf("advance=%d token=%q, want advance=4 token=\"abc\"", advance, token)
	}
}

func TestScanFFmpegLines_CRLF(t *testing.T) {
	advance, token, err := scanFFmpegLines([]byte("abc\r\ndef"), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if advance != 5 || string(token) != "abc" {
		t.Errorf("advance=%d token=%q, want advance=5 token=\"abc\" (CRLF consumed as a single terminator)", advance, token)
	}
}

func TestScanFFmpegLines_TrailingDataAtEOF(t *testing.T) {
	advance, token, err := scanFFmpegLines([]byte("trailing"), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if advance != len("trailing") || string(token) != "trailing" {
		t.Errorf("advance=%d token=%q, want advance=%d token=\"trailing\"", advance, token, len("trailing"))
	}
}

func TestScanFFmpegLines_RequestsMoreDataWhenNoTerminatorYet(t *testing.T) {
	advance, token, err := scanFFmpegLines([]byte("partial"), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if advance != 0 || token != nil {
		t.Errorf("advance=%d token=%q, want 0/nil (request more data)", advance, token)
	}
}

func TestScanFFmpegLines_EmptyAtEOF(t *testing.T) {
	advance, token, err := scanFFmpegLines([]byte{}, true)
	if err != nil || advance != 0 || token != nil {
		t.Errorf("got advance=%d token=%q err=%v, want 0/nil/nil", advance, token, err)
	}
}

func TestScanFFmpegLines_FullScanMixedTerminators(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("a\r\nb\rc\nd"))
	scanner.Split(scanFFmpegLines)
	var got []string
	for scanner.Scan() {
		got = append(got, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("unexpected scanner error: %v", err)
	}
	want := []string{"a", "b", "c", "d"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// buildRealisticFFmpegStderr constructs synthetic ffmpeg stderr mirroring
// real-world output: banner lines terminated with '\n', followed by many
// periodic "frame=... time=..." refreshes terminated with a bare '\r' (as
// real ffmpeg does, per C13), and a final '\n'-terminated summary line.
// Returns the raw bytes and the "time=" value (seconds) of the last
// progress line, so tests can assert the whole stream was scanned.
func buildRealisticFFmpegStderr(numFrames int, secsPerFrame float64) ([]byte, float64) {
	var b bytes.Buffer
	b.WriteString("ffmpeg version 6.0 Copyright (c) 2000-2024 the FFmpeg developers\n")
	b.WriteString("  built with gcc 12.2.1\n")
	b.WriteString("Input #0, mov,mp4,m4a,3gp,3g2,mj2, from 'input.mp4':\n")
	b.WriteString("  Duration: 00:30:00.00, start: 0.000000, bitrate: 5000 kb/s\n")

	var lastSecs float64
	for i := 1; i <= numFrames; i++ {
		lastSecs = float64(i) * secsPerFrame
		h := int(lastSecs) / 3600
		mn := (int(lastSecs) % 3600) / 60
		s := lastSecs - float64(h*3600+mn*60)
		fmt.Fprintf(&b, "frame=%5d fps= 30 q=28.0 size=%8dkB time=%02d:%02d:%05.2f bitrate=1024.0kbits/s speed=1.01x    \r", i, i*10, h, mn, s)
	}
	b.WriteString("video:512000kB audio:8300kB subtitle:0kB other streams:0kB global headers:0kB muxing overhead: 0.017535%\n")
	return b.Bytes(), lastSecs
}

// TestScanFFmpegLines_NoErrTooLongOnLargeCRStream directly reproduces the
// C13 payload shape (>100KB of bare-'\r'-terminated stats with no
// intervening '\n') and asserts the split func breaks it into many small
// tokens rather than one oversized token that would trip bufio.ErrTooLong.
func TestScanFFmpegLines_NoErrTooLongOnLargeCRStream(t *testing.T) {
	data, _ := buildRealisticFFmpegStderr(2000, 0.3)
	if len(data) < 100*1024 {
		t.Fatalf("test fixture too small to reproduce C13 (%d bytes, want > 100KB)", len(data))
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Split(scanFFmpegLines)
	lines := 0
	for scanner.Scan() {
		lines++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner.Err() = %v, want nil (no bufio.ErrTooLong)", err)
	}
	if lines < 2000 {
		t.Errorf("expected at least 2000 scanned tokens, got %d", lines)
	}
}

// TestMonitorProgress_HandlesCarriageReturnTerminatedStats feeds a realistic,
// >100KB ffmpeg stderr stream (bare-'\r'-terminated progress lines) through
// monitorProgress end-to-end and confirms: progress updates actually happen,
// the stderr reader is fully drained, and no scanning error occurs. Before
// the C13 fix, the entire '\r'-only block coalesced into a single scanner
// token that never triggered a progress update and eventually exceeded
// bufio.MaxScanTokenSize (ErrTooLong), leaving Progress stuck at 0.
func TestMonitorProgress_HandlesCarriageReturnTerminatedStats(t *testing.T) {
	const numFrames = 1500
	data, lastSecs := buildRealisticFFmpegStderr(numFrames, 0.4)
	if len(data) < 100*1024 {
		t.Fatalf("test fixture too small to reproduce C13 (%d bytes, want > 100KB)", len(data))
	}

	m := &Module{
		jobs: map[string]*models.HLSJob{
			"job1": {ID: "job1", Status: models.HLSStatusRunning, Progress: 0},
		},
		log: logger.New("test"),
	}
	run := &qualityRunParams{JobID: "job1", TotalQualities: 1, CurrentQuality: 1, TotalDuration: lastSecs + 1}

	reader := bytes.NewReader(data)
	m.monitorProgress("job1", reader, run)

	if reader.Len() != 0 {
		t.Errorf("stderr reader not fully drained: %d bytes remain", reader.Len())
	}

	got := m.jobs["job1"].Progress
	want := 100.0 * calculateVariantProgress(lastSecs, run.TotalDuration)
	if got < 50 {
		t.Errorf("job progress = %f, want a large value reflecting the full \\r-terminated stream having been scanned (stuck-at-0 indicates the pre-fix bug)", got)
	}
	if got < want-0.5 || got > want+0.5 {
		t.Errorf("job progress = %f, want ~%f (last parsed time=%.2fs)", got, want, lastSecs)
	}
}

// ---------------------------------------------------------------------------
// stderrTailBuffer (C13: bound the stderr tee instead of an unbounded bytes.Buffer)
// ---------------------------------------------------------------------------

func TestStderrTailBuffer_KeepsFullContentWhenUnderLimit(t *testing.T) {
	var buf stderrTailBuffer
	if _, err := buf.Write([]byte("hello world")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.String() != "hello world" {
		t.Errorf("String() = %q, want %q", buf.String(), "hello world")
	}
}

func TestStderrTailBuffer_KeepsOnlyTailWhenOverLimit(t *testing.T) {
	var buf stderrTailBuffer
	// Write far more than maxStderrTailBytes in small chunks, mirroring how
	// io.TeeReader feeds one scanner-line-sized chunk at a time.
	chunk := []byte("0123456789") // 10 bytes/write
	var want bytes.Buffer
	for want.Len() < maxStderrTailBytes*3 {
		if _, err := buf.Write(chunk); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want.Write(chunk)
	}

	if len(buf.buf) > maxStderrTailBytes {
		t.Fatalf("tail buffer grew to %d bytes, want <= %d", len(buf.buf), maxStderrTailBytes)
	}

	wantAll := want.String()
	wantTail := wantAll[len(wantAll)-maxStderrTailBytes:]
	if buf.String() != wantTail {
		t.Errorf("tail buffer content is not the tail of everything written (got len=%d, want len=%d)", len(buf.String()), len(wantTail))
	}
}

// ---------------------------------------------------------------------------
// calculateVariantProgress unknown-duration branch (C21: no 50% jump)
// ---------------------------------------------------------------------------

func TestCalculateVariantProgress_UnknownDuration_NoJumpOnFirstSample(t *testing.T) {
	// Regression test for C21: the unknown-duration branch used to have a 0.5
	// floor, so the very first non-zero sample (e.g. one 6s segment in) jumped
	// straight to ~50%. It should now start near 0 and rise smoothly instead.
	got := calculateVariantProgress(6, 0)
	if got <= 0 {
		t.Errorf("calculateVariantProgress(6, 0) = %f, want > 0", got)
	}
	if got >= 0.1 {
		t.Errorf("calculateVariantProgress(6, 0) = %f, want a small value near 0 (no 50%% jump)", got)
	}
}

func TestCalculateVariantProgress_UnknownDuration_RisesMonotonically(t *testing.T) {
	prev := 0.0
	for _, secs := range []float64{1, 60, 600, 1800, 3600, 7200, 20000} {
		got := calculateVariantProgress(secs, 0)
		if got < prev {
			t.Errorf("calculateVariantProgress(%v, 0) = %f, want >= previous %f (monotonic rise)", secs, got, prev)
		}
		prev = got
	}
}

func TestCalculateVariantProgress_UnknownDuration_CapsAtMaxPct(t *testing.T) {
	got := calculateVariantProgress(1_000_000, 0)
	if got > 0.95 {
		t.Errorf("calculateVariantProgress with unknown duration should cap at 0.95, got %f", got)
	}
	if got < 0.94 {
		t.Errorf("calculateVariantProgress for a huge currentSecs should be at/near the 0.95 cap, got %f", got)
	}
}
