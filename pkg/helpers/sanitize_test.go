package helpers

import (
	"strings"
	"testing"
)

func TestSanitizeString(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"hello", "hello"},
		{"<script>alert(1)</script>", "&lt;script&gt;alert(1)&lt;/script&gt;"},
		{"foo\x00bar", "foobar"}, // null bytes stripped
		{`"quoted"`, "&#34;quoted&#34;"},
		{"a & b", "a &amp; b"},
	} {
		if got := SanitizeString(tc.in); got != tc.want {
			t.Errorf("SanitizeString(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidateMetadataKey(t *testing.T) {
	for _, tc := range []struct {
		key  string
		want bool
	}{
		{"title", true},
		{"my-key", true},
		{"my_key", true},
		{"key.123", true},
		{"", false},
		{strings.Repeat("a", 101), false}, // too long
		{"has space", false},
		{"has@symbol", false},
	} {
		if got := ValidateMetadataKey(tc.key); got != tc.want {
			t.Errorf("ValidateMetadataKey(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

func TestSafeContentDispositionFilename(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{"video.mp4", `attachment; filename="video.mp4"`},
		{`has"quote.mp4`, `attachment; filename="hasquote.mp4"`},
		{"has\\back.mp4", `attachment; filename="hasback.mp4"`},
		{"has\nnewline.mp4", `attachment; filename="hasnewline.mp4"`},
		{"has\x01control.mp4", `attachment; filename="hascontrol.mp4"`},
		{"normal name.mp4", `attachment; filename="normal name.mp4"`},
	} {
		if got := SafeContentDispositionFilename(tc.input); got != tc.want {
			t.Errorf("SafeContentDispositionFilename(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestValidateMetadataValue(t *testing.T) {
	if !ValidateMetadataValue("short") {
		t.Error("ValidateMetadataValue(short) should be true")
	}
	if !ValidateMetadataValue(strings.Repeat("x", 10240)) {
		t.Error("ValidateMetadataValue(10240 chars) should be true (at limit)")
	}
	if ValidateMetadataValue(strings.Repeat("x", 10241)) {
		t.Error("ValidateMetadataValue(10241 chars) should be false (over limit)")
	}
}
