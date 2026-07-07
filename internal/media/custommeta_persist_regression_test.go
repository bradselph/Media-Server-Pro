package media

import (
	"reflect"
	"testing"
	"time"
)

// TestConvertMetadata_CustomMetaRoundTrip guards that admin-set custom metadata
// survives the internal<->repo conversion that persists to and reloads from the
// database. Before the fix, convertInternalToRepo dropped CustomMeta entirely, so
// custom fields (e.g. a description) were lost on every restart/rescan.
func TestConvertMetadata_CustomMetaRoundTrip(t *testing.T) {
	m := &Module{}
	orig := &Metadata{
		StableID:   "id-1",
		DateAdded:  time.Now(),
		CustomMeta: map[string]string{"description": "A great video", "studio": "acme"},
	}

	repoMeta := m.convertInternalToRepo("/videos/foo.mp4", orig)
	if !reflect.DeepEqual(repoMeta.CustomMeta, orig.CustomMeta) {
		t.Fatalf("convertInternalToRepo dropped custom meta: want %v, got %v", orig.CustomMeta, repoMeta.CustomMeta)
	}

	// The repo copy must be independent of the live map — convertInternalToRepo
	// runs under a read lock but the map is marshaled later, after the lock is
	// released, so a shared reference would race a concurrent in-place edit.
	orig.CustomMeta["description"] = "MUTATED"
	if repoMeta.CustomMeta["description"] != "A great video" {
		t.Fatal("convertInternalToRepo must deep-copy CustomMeta, not share the live map")
	}

	back := m.convertRepoToInternal(repoMeta)
	if back.CustomMeta["description"] != "A great video" || back.CustomMeta["studio"] != "acme" {
		t.Fatalf("convertRepoToInternal did not restore custom meta: got %v", back.CustomMeta)
	}
}
