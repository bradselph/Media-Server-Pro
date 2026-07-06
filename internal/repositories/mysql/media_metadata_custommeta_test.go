package mysql

import (
	"reflect"
	"testing"

	"media-server-pro/internal/repositories"
)

// TestCustomMetaMarshalRoundTrip verifies the JSON encode/decode helpers for the
// custom_meta column: a populated map round-trips exactly, and empty/nil inputs
// collapse to the empty string (stored NULL-like) and back to nil.
func TestCustomMetaMarshalRoundTrip(t *testing.T) {
	in := map[string]string{"description": "A great video", "director": "Ada L."}
	s := marshalCustomMeta(in)
	if s == "" {
		t.Fatal("non-empty custom meta must marshal to a non-empty string")
	}
	out := unmarshalCustomMeta(s)
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("custom meta did not round-trip: in=%v out=%v", in, out)
	}

	if got := marshalCustomMeta(nil); got != "" {
		t.Errorf("nil map should marshal to empty string, got %q", got)
	}
	if got := marshalCustomMeta(map[string]string{}); got != "" {
		t.Errorf("empty map should marshal to empty string, got %q", got)
	}
	if got := unmarshalCustomMeta(""); got != nil {
		t.Errorf("empty column should unmarshal to nil, got %v", got)
	}
	if got := unmarshalCustomMeta("not json"); got != nil {
		t.Errorf("unparseable column should unmarshal to nil, got %v", got)
	}
}

// TestCustomMetaRowRoundTrip verifies the full row mapping: a MediaMetadata with
// custom fields survives buildMetadataRow -> (column) -> rowToMetadata unchanged.
// This is the persistence path that previously dropped custom metadata entirely.
func TestCustomMetaRowRoundTrip(t *testing.T) {
	orig := &repositories.MediaMetadata{
		Path:       "/videos/foo.mp4",
		StableID:   "id-1",
		DateAdded:  "2026-01-02T03:04:05Z",
		CustomMeta: map[string]string{"description": "hello", "year": "2026"},
	}
	row := buildMetadataRow(orig.Path, orig)
	if row.CustomMeta == "" {
		t.Fatal("buildMetadataRow must serialize custom_meta")
	}

	repo := &MediaMetadataRepository{}
	got := repo.rowToMetadata(&row)
	if !reflect.DeepEqual(orig.CustomMeta, got.CustomMeta) {
		t.Fatalf("custom meta lost across row mapping: want %v, got %v", orig.CustomMeta, got.CustomMeta)
	}
}
