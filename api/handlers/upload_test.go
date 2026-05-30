package handlers

import (
	"errors"
	"testing"
)

// userUploadError must surface validation failures verbatim (so the uploader
// knows what to fix) while keeping lower-level I/O errors generic (so internal
// filesystem paths are never leaked to the client).
func TestUserUploadError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, "Upload failed"},
		{"type", errors.New("file type not allowed: .png"), "file type not allowed: .png"},
		{"content", errors.New("file content does not match extension (detected text/html)"), "file content does not match extension (detected text/html)"},
		{"size", errors.New("file size 123 bytes exceeds maximum allowed size of 100 bytes"), "file size 123 bytes exceeds maximum allowed size of 100 bytes"},
		{"filename", errors.New("invalid filename"), "invalid filename"},
		{"traversal", errors.New("path traversal detected"), "path traversal detected"},
		// Low-level I/O errors carry filesystem paths and must stay generic.
		{"io_leak", errors.New("upload failed: failed to finalize upload: rename /uploads/u1/x.mp4.tmp: no space left on device"), "Upload failed"},
		{"open_leak", errors.New("failed to open file: /tmp/multipart-123: permission denied"), "Upload failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := userUploadError(tc.err); got != tc.want {
				t.Errorf("userUploadError(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}
