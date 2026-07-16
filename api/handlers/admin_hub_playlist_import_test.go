package handlers

import (
	"testing"

	"media-server-pro/internal/downloader"
)

// TestHubDownloadInQueue verifies the queue-correlation used to detect when a
// started download has left the downloader queue (the completion signal for the
// playlist import job).
func TestHubDownloadInQueue(t *testing.T) {
	q := &downloader.QueueResponse{
		Active: []downloader.QueueActiveItem{{DownloadID: "active-1"}},
		Queued: []downloader.QueueQueuedItem{{DownloadID: "queued-1"}},
	}
	cases := []struct {
		q    *downloader.QueueResponse
		id   string
		want bool
	}{
		{q, "active-1", true},    // in Active
		{q, "queued-1", true},    // in Queued
		{q, "missing", false},    // finished / never seen
		{nil, "active-1", false}, // nil queue = nothing in flight
		{&downloader.QueueResponse{}, "active-1", false},
	}
	for _, c := range cases {
		if got := hubDownloadInQueue(c.q, c.id); got != c.want {
			t.Errorf("hubDownloadInQueue(%v) = %v, want %v", c.id, got, c.want)
		}
	}
}

// TestHubPlaylistImportJob_Snapshot verifies the job's counters and that an
// empty result set serializes as a non-nil slice (frontend expects an array).
func TestHubPlaylistImportJob_Snapshot(t *testing.T) {
	job := newHubPlaylistImportJob("pl1", "My Playlist", 3, func() {})
	if st := job.snapshot(); st.Results == nil {
		t.Error("empty snapshot Results should be non-nil ([]), got nil")
	}
	job.record(hubPlaylistImportResult{EmbedID: "a", Status: hubImportStatusImported})
	job.record(hubPlaylistImportResult{EmbedID: "b", Status: hubImportStatusFailed})
	job.record(hubPlaylistImportResult{EmbedID: "c", Status: hubImportStatusSkipped})
	job.finish(false)
	st := job.snapshot()
	if st.Running {
		t.Error("finished job should not be running")
	}
	if st.Done != 3 || st.Imported != 1 || st.Failed != 1 || st.Skipped != 1 {
		t.Errorf("counters = done %d/imp %d/fail %d/skip %d, want 3/1/1/1", st.Done, st.Imported, st.Failed, st.Skipped)
	}
	if st.PlaylistID != "pl1" || st.PlaylistName != "My Playlist" {
		t.Errorf("playlist meta = %q/%q", st.PlaylistID, st.PlaylistName)
	}
}
