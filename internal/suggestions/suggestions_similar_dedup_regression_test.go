package suggestions

import "testing"

// TestGetSimilarMedia_NoDuplicateFiller guards the fix for GetSimilarMedia
// returning the same MediaID twice. When too few genuinely-similar items are
// found (< limit/2), the result is padded with randomSample, which only excludes
// the source item — so it can re-draw items already present as real matches.
// Nothing downstream (sort/topShuffled/diversify) de-dupes by MediaID, so those
// re-draws surfaced as duplicate entries in the "Similar Media" sidebar.
func TestGetSimilarMedia_NoDuplicateFiller(t *testing.T) {
	// Source A plus two genuinely-similar items (B, C: same category + type,
	// score > 0) and one dissimilar item (D: score 0, so it only ever appears as
	// filler). With limit=10, limit/2=5 and only 2 real matches, the filler path
	// runs; randomSample's pool is {B,C,D} (<= limit) so it always re-draws B and
	// C — exactly the duplication the fix must suppress.
	a := &MediaInfo{StableID: "A", Path: "/a", Title: "A", MediaType: "video", CategoryIDs: []string{"cat1"}}
	b := &MediaInfo{StableID: "B", Path: "/b", Title: "B", MediaType: "video", CategoryIDs: []string{"cat1"}}
	c := &MediaInfo{StableID: "C", Path: "/c", Title: "C", MediaType: "video", CategoryIDs: []string{"cat1"}}
	d := &MediaInfo{StableID: "D", Path: "/d", Title: "D", MediaType: "audio", CategoryIDs: []string{"cat2"}}

	m := &Module{
		mediaData: map[string]*MediaInfo{"/a": a, "/b": b, "/c": c, "/d": d},
		mediaByID: map[string]*MediaInfo{"A": a, "B": b, "C": c, "D": d},
	}

	// Run several times: the filler order is randomized, but a correct
	// implementation must never emit a MediaID more than once regardless.
	for iter := range 50 {
		results := m.GetSimilarMedia("A", 10, true)
		seen := make(map[string]int, len(results))
		for _, r := range results {
			seen[r.MediaID]++
			if seen[r.MediaID] > 1 {
				t.Fatalf("iteration %d: MediaID %q appears %d times in GetSimilarMedia result; filler must not re-include real matches",
					iter, r.MediaID, seen[r.MediaID])
			}
		}
		// The source itself must never be suggested.
		if _, ok := seen["A"]; ok {
			t.Fatalf("iteration %d: source item A must not appear in its own similar list", iter)
		}
	}
}
