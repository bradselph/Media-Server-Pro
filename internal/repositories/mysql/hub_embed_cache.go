package mysql

import (
	"container/list"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Result caching for the Hub catalog.
//
// Browsing an unfiltered catalog page is cheap: every sort has a matching
// (sortkey, id) index and the row count comes from cachedTotal. Applying a
// category or tag filter is not. The filter predicate is a leading-wildcard LIKE
// over a wrapped column, which no index can satisfy, so MySQL reads the whole
// table — and hub_embeds rows are fat (thumb_url TEXT, preview_urls MEDIUMTEXT),
// so "the whole table" is gigabytes of I/O. Worse, Search() pays it twice per
// request: once for COUNT(*) and once for the page itself.
//
// This cache is the mitigation for repeat and concurrent reads of the same
// filter. It does not make the first read of a new filter fast — only an indexed
// filter path can do that (see hub_embed_categories) — but it does mean a viewer
// paging through a category, switching sorts, or coming back to it pays that cost
// at most once per TTL, and that N viewers who pick the same category pay it once
// between them rather than N times.
//
// Cached values are treated as immutable by every reader. Records handed out here
// are shared between callers, so nothing downstream may mutate them; hub.toEmbed
// only reads them and builds a fresh Embed, which is what makes this safe.

const (
	// hubFilterTTL bounds how long a filtered result is reused. The catalog only
	// changes on import or clear, and both invalidate this cache outright, so this
	// is a backstop for writes that bypass this process rather than the primary
	// correctness mechanism.
	hubFilterTTL = 5 * time.Minute

	// hubCountCacheMax bounds the count cache by entry count. Counts are a single
	// int64 each, so this can be generous — its real job is to survive an
	// arbitrary number of distinct free-text searches without unbounded growth.
	hubCountCacheMax = 4096

	// hubPageCacheMax bounds the page cache. Entries here are whole result pages
	// (up to 200 records, each carrying preview URLs), so roughly 150 KB per
	// entry at the default page size — this cap keeps the worst case in the tens
	// of megabytes rather than the hundreds.
	hubPageCacheMax = 256
)

// hubResultCache is a bounded, TTL'd LRU with singleflight coalescing.
//
// The eviction shape is copied from internal/hub.imageCache (container/list +
// map) rather than a plain map, because the key space includes free-text search
// terms: a map alone would grow with every distinct query a crawler or a bored
// user types.
type hubResultCache struct {
	mu         sync.Mutex
	ttl        time.Duration
	maxEntries int
	order      *list.List // front = most recently used
	items      map[string]*list.Element
	// generation is bumped by clear(). A query that started before an import
	// finished must not be allowed to write its pre-import answer back into the
	// cache afterwards — the invalidation would be silently undone and the stale
	// page would then be served for a full TTL. Writes carry the generation they
	// were issued under and are dropped if it has moved on.
	generation uint64
	// sf coalesces concurrent misses for the same key so a burst of viewers
	// selecting the same category runs one query, not one query each.
	sf singleflight.Group
}

type hubCacheEntry struct {
	key       string
	value     any
	expiresAt time.Time
}

func newHubResultCache(maxEntries int, ttl time.Duration) *hubResultCache {
	return &hubResultCache{
		ttl:        ttl,
		maxEntries: maxEntries,
		order:      list.New(),
		items:      make(map[string]*list.Element),
	}
}

// get returns the cached value when a non-expired entry exists. Expired entries
// are evicted on read, so no sweeper goroutine is needed.
func (c *hubResultCache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	e, _ := el.Value.(*hubCacheEntry)
	if e == nil {
		c.removeElement(el)
		return nil, false
	}
	if time.Now().After(e.expiresAt) {
		c.removeElement(el)
		return nil, false
	}
	c.order.MoveToFront(el)
	return e.value, true
}

// currentGeneration returns the generation a read is being issued under.
func (c *hubResultCache) currentGeneration() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generation
}

// set stores a value, evicting least-recently-used entries past the bound. The
// write is dropped when gen is stale, i.e. the catalog changed while the query
// that produced value was still running.
func (c *hubResultCache) set(key string, value any, gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.generation {
		return
	}
	expiresAt := time.Now().Add(c.ttl)
	if el, ok := c.items[key]; ok {
		if e, _ := el.Value.(*hubCacheEntry); e != nil {
			e.value = value
			e.expiresAt = expiresAt
		}
		c.order.MoveToFront(el)
		return
	}
	el := c.order.PushFront(&hubCacheEntry{key: key, value: value, expiresAt: expiresAt})
	c.items[key] = el
	for c.maxEntries > 0 && c.order.Len() > c.maxEntries {
		back := c.order.Back()
		if back == nil {
			break
		}
		c.removeElement(back)
	}
}

// removeElement drops one element. Callers must hold c.mu.
func (c *hubResultCache) removeElement(el *list.Element) {
	c.order.Remove(el)
	if e, _ := el.Value.(*hubCacheEntry); e != nil {
		delete(c.items, e.key)
	}
}

// clear drops every entry. Called after any write to the catalog: an import or a
// clear changes enough rows that per-key invalidation would be guesswork.
func (c *hubResultCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.order.Init()
	c.items = make(map[string]*list.Element)
	// Anything still in flight was computed against the pre-clear catalog; the
	// bump makes its write-back a no-op.
	c.generation++
}

// hubMemo is the canonical read path: serve from cache, else compute exactly once
// across concurrent callers and store the result.
//
// Errors are deliberately not cached — a transient DB failure must not pin an
// error for the whole TTL — but they are shared with everyone coalesced into the
// same flight, which is correct: they all asked the same question.
func hubMemo[T any](c *hubResultCache, key string, compute func() (T, error)) (T, error) {
	if v, ok := c.get(key); ok {
		if typed, ok := v.(T); ok {
			return typed, nil
		}
	}
	v, err, _ := c.sf.Do(key, func() (any, error) {
		// An earlier winner may have populated the cache between the fast-path
		// miss above and entry into Do.
		if v, ok := c.get(key); ok {
			if typed, ok := v.(T); ok {
				return typed, nil
			}
		}
		// Read the generation BEFORE the query runs, so an invalidation that
		// lands while it is running invalidates this result too.
		gen := c.currentGeneration()
		out, cErr := compute()
		if cErr != nil {
			return nil, cErr
		}
		c.set(key, out, gen)
		return out, nil
	})
	if err != nil {
		var zero T
		return zero, err
	}
	typed, ok := v.(T)
	if !ok {
		var zero T
		return zero, nil
	}
	return typed, nil
}

// writeField appends one length-prefixed field to a key.
//
// Length-prefixing rather than separating: query, category and tag arrive
// straight from HTTP query parameters, and net/url decodes %00 into a real NUL
// byte, so no byte value is safe to reserve as a delimiter. With an explicit
// length there is no delimiter to forge — ("a", "b") and ("a\x00b", "") encode
// differently no matter what bytes they contain, so one filter can never be
// served another's cached results.
func writeField(b *strings.Builder, s string) {
	b.WriteString(strconv.Itoa(len(s)))
	b.WriteByte(':')
	b.WriteString(s)
}

// hubFilterKey builds the cache key identifying a filtered result set. Only the
// predicate is included, so every page and every sort of the same filter shares
// one count.
func hubFilterKey(query, category, tag string) string {
	var b strings.Builder
	writeField(&b, query)
	writeField(&b, category)
	writeField(&b, tag)
	return b.String()
}

// hubPageKey extends a filter key with everything that selects which rows come
// back: the sort and the window.
func hubPageKey(filterKey, sort string, offset, limit int) string {
	var b strings.Builder
	writeField(&b, filterKey)
	writeField(&b, sort)
	writeField(&b, strconv.Itoa(offset))
	writeField(&b, strconv.Itoa(limit))
	return b.String()
}
