package crawler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeRaceLogger is a minimal loggerI implementation for the reproduction test.
type fakeRaceLogger struct{}

func (fakeRaceLogger) Info(format string, args ...any)  {}
func (fakeRaceLogger) Debug(format string, args ...any) {}
func (fakeRaceLogger) Warn(format string, args ...any)  {}
func (fakeRaceLogger) Error(format string, args ...any) {}

// TestProbe_StreamsRace is a REPRODUCER for the claimed data race between the
// background network-event goroutine (browser.go:317, appends to
// result.Streams under streamsMu) and the unsynchronized read of
// result.Streams at browser.go:420 (and, after return, by callers such as
// crawler.go probeForStreams).
//
// The served page continuously fires fetch() requests to distinct URLs that
// are answered with an .m3u8 content-type, so the background goroutine is
// perpetually appending to result.Streams for the entire lifetime of the
// probe, including through the final "let events settle" window and the
// bd.log.Info call that reads len(result.Streams) without holding streamsMu.
//
// Run with -race to observe the detector flag the race:
//
//	go test ./internal/crawler/ -run TestProbe_StreamsRace -race -v
func TestProbe_StreamsRace(t *testing.T) {
	chromeBin := findChromeBinary()
	if chromeBin == "" {
		t.Skip("no Chrome/Chromium binary found; skipping live CDP reproduction")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!DOCTYPE html>
<html><head><title>race repro</title></head>
<body>
<script>
  var i = 0;
  setInterval(function() {
    i++;
    fetch('/s/' + i).catch(function(){});
  }, 1);
</script>
</body></html>`))
	})
	mux.HandleFunc("/s/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "#EXTM3U\n")
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	bd := &browserDetector{
		log:       fakeRaceLogger{},
		timeout:   45 * time.Second,
		chromeBin: chromeBin,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := bd.probe(ctx, ts.URL)
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}

	// Mirrors crawler.go probeForStreams' unsynchronized read of result.Streams
	// immediately after probe() returns — no lock is available to the caller.
	t.Logf("streams found: %d", len(result.Streams))
}
