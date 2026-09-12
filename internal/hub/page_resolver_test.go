package hub

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"media-server-pro/internal/config"
	"media-server-pro/internal/repositories"
)

// The page resolver is the only resolver that works without an external
// downloader service, so on a deployment with no sidecar it is the whole feature.
// It used to fetch only /embed/<id>, which is a thin player shell that usually
// carries no flashvars_ object — so it always failed, and server-side playback
// silently fell back to the provider iframe: exactly the case this feature exists
// to fix. These tests pin the page order.

// stubTransport answers requests from a fixed URL->body table and records the
// order in which URLs were requested. Any URL not in the table 404s.
type stubTransport struct {
	bodies    map[string]string
	requested []string
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	url := req.URL.String()
	s.requested = append(s.requested, url)
	body, ok := s.bodies[url]
	if !ok {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     "404 Not Found",
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

// pageResolverModule builds a Module whose chain is the page resolver only, with
// its upstream HTTP calls served by stub.
func pageResolverModule(t *testing.T, stub *stubTransport, embedIDs ...string) *Module {
	t.Helper()
	cfg := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	if err := cfg.Update(func(c *config.Config) {
		c.Features.EnableHub = true
		c.Hub.ProxyEnabled = true
		c.Hub.ProxyResolvers = []string{"page"}
	}); err != nil {
		t.Fatalf("configure hub: %v", err)
	}
	repo := &catalogRepo{byID: map[string]*repositories.HubEmbedRecord{}}
	for _, id := range embedIDs {
		repo.byID[id] = &repositories.HubEmbedRecord{EmbedID: id, Title: id}
	}
	m := NewModule(cfg, nil)
	m.repo = repo
	m.httpClient = &http.Client{Transport: stub}
	return m
}

// playerPage is a minimal stand-in for a provider page: a flashvars_ assignment
// carrying one HLS media definition.
const playerPage = `<html><script>
var flashvars_123 = {"mediaDefinitions":[{"format":"hls","quality":"1080","videoUrl":"https://cdn.example.com/master.m3u8"}]};
</script></html>`

// The watch page must be tried before the embed page, because it is the one that
// actually carries the player configuration.
func TestPageResolver_PrefersWatchPageOverEmbedPage(t *testing.T) {
	stub := &stubTransport{bodies: map[string]string{
		providerPageURL + "abc123": playerPage,
		embedBaseURL + "abc123":    playerPage,
	}}
	m := pageResolverModule(t, stub, "abc123")

	rs, err := m.ResolveStream(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if rs.Kind != StreamHLS || rs.URL != "https://cdn.example.com/master.m3u8" {
		t.Fatalf("unexpected stream: %+v", rs)
	}
	if len(stub.requested) != 1 {
		t.Fatalf("expected a single request, got %v", stub.requested)
	}
	if stub.requested[0] != providerPageURL+"abc123" {
		t.Fatalf("watch page should be tried first, got %q", stub.requested[0])
	}
}

// When the watch page yields nothing the embed page is still tried, so this
// change can only ever add coverage relative to the previous behaviour.
func TestPageResolver_FallsBackToEmbedPage(t *testing.T) {
	stub := &stubTransport{bodies: map[string]string{
		// Watch page present but carrying no player config.
		providerPageURL + "abc123": "<html>video unavailable</html>",
		embedBaseURL + "abc123":    playerPage,
	}}
	m := pageResolverModule(t, stub, "abc123")

	rs, err := m.ResolveStream(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if rs.URL != "https://cdn.example.com/master.m3u8" {
		t.Fatalf("unexpected stream URL: %q", rs.URL)
	}
	if len(stub.requested) != 2 {
		t.Fatalf("expected both pages to be tried, got %v", stub.requested)
	}
	if stub.requested[1] != embedBaseURL+"abc123" {
		t.Fatalf("embed page should be the second attempt, got %q", stub.requested[1])
	}
}

// Both pages failing must surface as an error, not a nil stream — the handler
// turns that into "keep showing the iframe".
func TestPageResolver_BothPagesFail(t *testing.T) {
	stub := &stubTransport{bodies: map[string]string{}}
	m := pageResolverModule(t, stub, "abc123")

	if _, err := m.ResolveStream(context.Background(), "abc123"); err == nil {
		t.Fatal("expected an error when neither page carries a stream")
	}
	if len(stub.requested) != 2 {
		t.Fatalf("expected both pages to be attempted, got %v", stub.requested)
	}
}
