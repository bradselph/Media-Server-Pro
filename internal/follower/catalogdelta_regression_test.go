package follower

import "testing"

// TestCatalogByID_DetectsChanges is a regression test for the catalog delta
// detection that replaced re-sending the entire catalog on every change. An item
// whose master-visible fields (Path/Name/Size/ContentFingerprint) are unchanged must
// hash identically across scans; a change to any of those fields must produce a
// different hash so only that item is pushed as a delta.
func TestCatalogByID_DetectsChanges(t *testing.T) {
	base := []*catalogItem{
		{ID: "a", Path: "v/a.mp4", Name: "A", Size: 100, ContentFingerprint: "fpa"},
		{ID: "b", Path: "v/b.mp4", Name: "B", Size: 200, ContentFingerprint: "fpb"},
	}
	first := catalogByID(base)

	// Unchanged rebuild → identical hashes (no delta would be sent).
	same := catalogByID([]*catalogItem{
		{ID: "a", Path: "v/a.mp4", Name: "A", Size: 100, ContentFingerprint: "fpa"},
		{ID: "b", Path: "v/b.mp4", Name: "B", Size: 200, ContentFingerprint: "fpb"},
	})
	for id, h := range first {
		if same[id] != h {
			t.Errorf("unchanged item %s hashed differently across scans", id)
		}
	}

	// Change item b's size → only b's hash changes.
	changed := catalogByID([]*catalogItem{
		{ID: "a", Path: "v/a.mp4", Name: "A", Size: 100, ContentFingerprint: "fpa"},
		{ID: "b", Path: "v/b.mp4", Name: "B", Size: 999, ContentFingerprint: "fpb"},
	})
	if changed["a"] != first["a"] {
		t.Error("item a was unchanged but its hash changed")
	}
	if changed["b"] == first["b"] {
		t.Error("item b's size changed but its hash did not")
	}
}
