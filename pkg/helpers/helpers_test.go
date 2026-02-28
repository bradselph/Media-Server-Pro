package helpers

import "testing"

func TestStatusString(t *testing.T) {
	if got := StatusString(true); got != "healthy" {
		t.Errorf("StatusString(true) = %q, want %q", got, "healthy")
	}
	if got := StatusString(false); got != "unhealthy" {
		t.Errorf("StatusString(false) = %q, want %q", got, "unhealthy")
	}
}

func TestIsMediaExtension(t *testing.T) {
	for _, tc := range []struct {
		ext  string
		want bool
	}{
		{".mp4", true},
		{".MP4", true}, // case-insensitive
		{".mkv", true},
		{".mp3", true},
		{".flac", true},
		{".txt", false},
		{".pdf", false},
		{"", false},
		{".exe", false},
	} {
		if got := IsMediaExtension(tc.ext); got != tc.want {
			t.Errorf("IsMediaExtension(%q) = %v, want %v", tc.ext, got, tc.want)
		}
	}
}

func TestIsAudioExtension(t *testing.T) {
	for _, tc := range []struct {
		ext  string
		want bool
	}{
		{".mp3", true},
		{".MP3", true}, // case-insensitive
		{".flac", true},
		{".ogg", true},
		// Video containers must NOT be flagged as audio-only
		{".mp4", false},
		{".mkv", false},
		{".avi", false},
		{".txt", false},
	} {
		if got := IsAudioExtension(tc.ext); got != tc.want {
			t.Errorf("IsAudioExtension(%q) = %v, want %v", tc.ext, got, tc.want)
		}
	}
}
