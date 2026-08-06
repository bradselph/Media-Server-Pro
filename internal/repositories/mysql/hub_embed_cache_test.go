package mysql

import (
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"
)

// The filtered Hub path runs an unindexed full-table scan over millions of fat
// rows, so every one of these properties is the difference between a page that
// loads and one that takes a minute: a hit must not recompute, concurrent misses
// must not all recompute, and a transient error must not be remembered as an
// answer.

func TestHubResultCache_HitAvoidsRecompute(t *testing.T) {
	c := newHubResultCache(16, time.Minute)
	var calls int

	compute := func() (int64, error) {
		calls++
		return 42, nil
	}
	for i := range 5 {
		got, err := hubMemo(c, "k", compute)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if got != 42 {
			t.Fatalf("call %d: got %d, want 42", i, got)
		}
	}
	if calls != 1 {
		t.Fatalf("compute ran %d times, want 1", calls)
	}
}

func TestHubResultCache_ExpiredEntryRecomputes(t *testing.T) {
	c := newHubResultCache(16, time.Millisecond)
	var calls int
	compute := func() (int64, error) {
		calls++
		return int64(calls), nil
	}

	if _, err := hubMemo(c, "k", compute); err != nil {
		t.Fatalf("first: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	got, err := hubMemo(c, "k", compute)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if calls != 2 || got != 2 {
		t.Fatalf("expired entry not recomputed: calls=%d got=%d", calls, got)
	}
}

// The key space includes free-text search terms, so an unbounded map would grow
// with every distinct query typed at the server.
func TestHubResultCache_EvictsLeastRecentlyUsed(t *testing.T) {
	const max = 4
	c := newHubResultCache(max, time.Minute)

	for i := range max {
		c.set(strconv.Itoa(i), int64(i), c.currentGeneration())
	}
	// Touch key 0 so it is no longer the eviction candidate.
	if _, ok := c.get("0"); !ok {
		t.Fatal("key 0 should still be cached")
	}
	c.set("new", int64(99), c.currentGeneration())

	if c.order.Len() > max {
		t.Fatalf("cache holds %d entries, want at most %d", c.order.Len(), max)
	}
	if _, ok := c.get("0"); !ok {
		t.Error("recently used key 0 was evicted")
	}
	if _, ok := c.get("1"); ok {
		t.Error("least recently used key 1 should have been evicted")
	}
	if _, ok := c.get("new"); !ok {
		t.Error("newly stored key missing")
	}
}

// A transient database failure must not be cached — otherwise one blip pins an
// error for the whole TTL and the catalog looks broken long after it recovered.
func TestHubResultCache_DoesNotCacheErrors(t *testing.T) {
	c := newHubResultCache(16, time.Minute)
	wantErr := errors.New("db down")
	var calls int

	compute := func() (int64, error) {
		calls++
		if calls == 1 {
			return 0, wantErr
		}
		return 7, nil
	}

	if _, err := hubMemo(c, "k", compute); !errors.Is(err, wantErr) {
		t.Fatalf("want the compute error, got %v", err)
	}
	got, err := hubMemo(c, "k", compute)
	if err != nil {
		t.Fatalf("retry after error: %v", err)
	}
	if got != 7 {
		t.Fatalf("got %d, want 7 — the failure should not have been cached", got)
	}
}

// N viewers picking the same category at once must cost one query, not N. This
// is the thundering-herd case: without coalescing, every one of them starts an
// independent full-table scan.
func TestHubResultCache_CoalescesConcurrentMisses(t *testing.T) {
	c := newHubResultCache(16, time.Minute)

	var mu sync.Mutex
	var calls int
	release := make(chan struct{})

	compute := func() (int64, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		<-release // hold the flight open so the others pile onto it
		return 5, nil
	}

	const workers = 8
	var wg sync.WaitGroup
	errs := make([]error, workers)
	vals := make([]int64, workers)
	for i := range workers {
		wg.Go(func() {
			vals[i], errs[i] = hubMemo(c, "same", compute)
		})
	}
	// Give the goroutines time to reach the singleflight before releasing.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 1 {
		t.Fatalf("compute ran %d times for %d concurrent callers, want 1", got, workers)
	}
	for i := range workers {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if vals[i] != 5 {
			t.Fatalf("worker %d got %d, want 5", i, vals[i])
		}
	}
}

func TestHubResultCache_ClearDropsEverything(t *testing.T) {
	c := newHubResultCache(16, time.Minute)
	c.set("a", int64(1), c.currentGeneration())
	c.set("b", int64(2), c.currentGeneration())
	c.clear()

	if _, ok := c.get("a"); ok {
		t.Error("entry survived clear()")
	}
	if c.order.Len() != 0 || len(c.items) != 0 {
		t.Errorf("clear() left %d ordered / %d mapped entries", c.order.Len(), len(c.items))
	}
}

// An import that lands while a query is still running must win. Otherwise the
// query writes its pre-import answer back after the invalidation and the stale
// page is served for a full TTL.
func TestHubResultCache_InvalidationBeatsInFlightCompute(t *testing.T) {
	c := newHubResultCache(16, time.Minute)

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		_, _ = hubMemo(c, "k", func() (int64, error) {
			close(started)
			<-release // an import lands while we are "querying"
			return 1, nil
		})
	}()

	<-started
	c.clear() // the catalog changed underneath the in-flight query
	close(release)
	<-done

	if _, ok := c.get("k"); ok {
		t.Fatal("a result computed before the invalidation was written back after it")
	}
}

// A NUL byte survives URL decoding into query parameters, so it cannot be used
// as a field separator. Length-prefixed encoding must keep these distinct.
func TestHubFilterKey_NulBytesInInputDoNotCollide(t *testing.T) {
	a := hubFilterKey("a\x00b", "", "")
	b := hubFilterKey("a", "b", "")
	if a == b {
		t.Fatalf("filters (%q,%q,%q) and (%q,%q,%q) collided onto key %q",
			"a\x00b", "", "", "a", "b", "", a)
	}

	c := hubFilterKey("", "Teen\x00", "x")
	d := hubFilterKey("", "Teen", "\x00x")
	if c == d {
		t.Fatalf("NUL-bearing category/tag pair collided onto key %q", c)
	}
}

// The same property must hold for the page key, whose fields include a nested
// filter key that itself contains the length-prefix separator ':'.
func TestHubPageKey_NestedSeparatorsDoNotCollide(t *testing.T) {
	seen := map[string]string{}
	for _, tc := range []struct {
		query, category, sort string
		offset, limit         int
	}{
		{"1:x", "", "views", 0, 60},
		{"", "1:x", "views", 0, 60},
		{"", "", "views:0", 60, 60},
		{"", "", "views", 0, 60},
	} {
		key := hubPageKey(hubFilterKey(tc.query, tc.category, ""), tc.sort, tc.offset, tc.limit)
		label := tc.query + "|" + tc.category + "|" + tc.sort
		if prev, ok := seen[key]; ok {
			t.Errorf("page key collision between %q and %q", prev, label)
		}
		seen[key] = label
	}
}

// Distinct filters must not collide onto one key — a collision would serve one
// category's results under another's name.
func TestHubFilterKey_NoCollisions(t *testing.T) {
	seen := map[string]string{}
	cases := []struct{ query, category, tag string }{
		{"a", "b", ""},
		{"a", "", "b"},
		{"", "a", "b"},
		{"ab", "", ""},
		{"", "ab", ""},
		{"", "", "ab"},
		{"a;b", "c", ""},
	}
	for _, c := range cases {
		key := hubFilterKey(c.query, c.category, c.tag)
		label := c.query + "|" + c.category + "|" + c.tag
		if prev, ok := seen[key]; ok {
			t.Errorf("key collision between %q and %q", prev, label)
		}
		seen[key] = label
	}
}

// The page key must separate sort and window; sharing a key across sorts would
// serve most-viewed rows to someone who asked for longest.
func TestHubPageKey_SeparatesSortAndWindow(t *testing.T) {
	base := hubFilterKey("", "Teen", "")
	seen := map[string]bool{}
	for _, c := range []struct {
		sort          string
		offset, limit int
	}{
		{"views", 0, 60},
		{"views", 60, 60},
		{"duration", 0, 60},
		{"views", 0, 6060}, // must not collide with {views, 60, 60}
		{"", 0, 60},
	} {
		key := hubPageKey(base, c.sort, c.offset, c.limit)
		if seen[key] {
			t.Errorf("page key collision at sort=%q offset=%d limit=%d", c.sort, c.offset, c.limit)
		}
		seen[key] = true
	}
}

// A write to the catalog must drop cached reads, or a fresh import serves the
// previous catalog for a full TTL.
func TestInvalidateTotal_ClearsFilteredCaches(t *testing.T) {
	r := &HubEmbedRepository{
		countCache: newHubResultCache(16, time.Minute),
		pageCache:  newHubResultCache(16, time.Minute),
	}
	r.countCache.set("count", int64(1), r.countCache.currentGeneration())
	r.pageCache.set("page", []*struct{}{}, r.pageCache.currentGeneration())
	r.storeTotal(123)

	r.invalidateTotal()

	if _, ok := r.countCache.get("count"); ok {
		t.Error("count cache survived invalidation")
	}
	if _, ok := r.pageCache.get("page"); ok {
		t.Error("page cache survived invalidation")
	}
	if r.totalValid {
		t.Error("unfiltered total still marked valid")
	}
}

// A repository built without the constructor (as some tests do) must not panic
// on the nil caches.
func TestInvalidateTotal_NilCachesAreSafe(t *testing.T) {
	r := &HubEmbedRepository{}
	r.invalidateTotal()
}
