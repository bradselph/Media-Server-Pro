package handlers

import "testing"

// TestOGThumbnailURL locks in the og:image URL contract: the social-card preview
// URL must carry ?og=1 (or &og=1 when a query already exists) so GetThumbnail
// serves the real thumbnail to unauthenticated crawlers instead of the censored
// "red box" placeholder. An empty input must stay empty so the shell's
// empty-thumb guard still suppresses og:image when an item has no thumbnail.
func TestOGThumbnailURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty stays empty", "", ""},
		{"existing query gets &og", "/thumbnail?id=abc123", "/thumbnail?id=abc123&og=1"},
		{"no query gets ?og", "/thumbnails/abc123.jpg", "/thumbnails/abc123.jpg?og=1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ogThumbnailURL(tc.in); got != tc.want {
				t.Errorf("ogThumbnailURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
