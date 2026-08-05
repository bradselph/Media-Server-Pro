package main

import (
	"context"

	"media-server-pro/internal/downloader"
	"media-server-pro/internal/hub"
)

// downloaderStreamDetector lets the Hub resolve a provider watch page into
// playable media URLs using the downloader service.
//
// It lives in the wiring layer on purpose. The Hub declares only a small
// interface (hub.StreamDetector) rather than importing the downloader package,
// so the two features stay independent: a deployment without the downloader
// still gets Hub playback via the built-in resolver, and the Hub's resolver
// chain remains unit-testable with a fake.
type downloaderStreamDetector struct {
	dl *downloader.Module
}

// DetectorReady reports whether the downloader service is configured, enabled,
// and currently reachable. A false here makes the Hub skip this resolver and
// fall through to the next one in the chain.
func (d *downloaderStreamDetector) DetectorReady() bool {
	return d != nil && d.dl != nil && d.dl.IsOnline()
}

// DetectStreams asks the downloader service to resolve pageURL.
//
// The context is accepted to satisfy the interface but not forwarded: the
// downloader client applies its own request timeout. The Hub bounds the whole
// resolution separately, so a hung service cannot stall playback indefinitely.
func (d *downloaderStreamDetector) DetectStreams(_ context.Context, pageURL string) ([]hub.DetectedStream, error) {
	if d == nil || d.dl == nil {
		return nil, nil
	}
	client := d.dl.GetClient()
	if client == nil {
		return nil, nil
	}
	resp, err := client.Detect(pageURL)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, nil
	}

	// Prefer the full candidate list; some responses carry only a single stream.
	candidates := resp.AllStreams
	if len(candidates) == 0 && resp.Stream != nil {
		candidates = []downloader.StreamInfo{*resp.Stream}
	}
	out := make([]hub.DetectedStream, 0, len(candidates))
	for _, s := range candidates {
		out = append(out, hub.DetectedStream{
			URL:        s.URL,
			Type:       s.Type,
			Quality:    s.Quality,
			Resolution: s.Resolution,
			Size:       s.Size,
			IsAd:       s.IsAd,
		})
	}
	return out, nil
}
