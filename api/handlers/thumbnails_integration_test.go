package handlers_test

import (
	"net/http"
	"testing"

	"media-server-pro/internal/testutil"
)

const (
	msgExpect503Thumbnails = "expected 503 (thumbnails module not available), got %d"
)

// TestGetThumbnail_MissingID tests thumbnail request without an id parameter.
func TestGetThumbnail_MissingID(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/thumbnail", nil)
	defer resp.Body.Close()

	// Thumbnails module not wired in test server — expect 503 (module unavailable)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf(msgExpect503Thumbnails, resp.StatusCode)
	}
}

// TestGetThumbnailPreviews_MissingID tests preview request without an id parameter.
func TestGetThumbnailPreviews_MissingID(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/thumbnails/previews", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf(msgExpect503Thumbnails, resp.StatusCode)
	}
}

// TestGetThumbnailBatch_MissingIDs tests batch thumbnail request without ids parameter.
func TestGetThumbnailBatch_MissingIDs(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/api/thumbnails/batch", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf(msgExpect503Thumbnails, resp.StatusCode)
	}
}

// TestServeThumbnailFile_InvalidFormat tests serving a thumbnail with unsupported extension.
func TestServeThumbnailFile_InvalidFormat(t *testing.T) {
	ts := testutil.NewTestServer(t)

	resp := ts.Request("GET", "/thumbnails/test.exe", nil)
	defer resp.Body.Close()

	// Either 503 (module not available) or 400 (invalid format)
	if resp.StatusCode != http.StatusServiceUnavailable && resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 503 or 400, got %d", resp.StatusCode)
	}
}
