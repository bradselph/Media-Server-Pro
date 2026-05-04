package analytics

import (
	"sync"
	"time"
)

// aggCache is a tiny in-memory TTL cache for the dashboard's hot
// aggregation endpoints (top-users, top-searches, error-paths, heatmap,
// device-breakdown, etc.). Each of those scans 50k-100k events per
// request — without a cache, the dashboard's auto-refresh + click-
// around behaviour hammers the analytics_events table for the same
// answer over and over.
//
// Keys are caller-defined strings so methods like GetTopUsers can build
// a key from their parameters ("topusers|metric=views|days=7|limit=10").
// Values are stored as `any` and asserted by the caller — keeping the
// cache generic avoids one cache-per-method.
//
// TTL is per-entry on insert; expired entries are evicted lazily on
// read so we don't need a sweeper goroutine for what's a small map.
type aggCache struct {
	mu      sync.Mutex
	entries map[string]aggCacheEntry
}

type aggCacheEntry struct {
	value     any
	expiresAt time.Time
}

func newAggCache() *aggCache {
	return &aggCache{entries: make(map[string]aggCacheEntry)}
}

// get returns the cached value and true if a non-expired entry exists.
// Expired entries are deleted in-place so the map doesn't accumulate.
func (c *aggCache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(e.expiresAt) {
		delete(c.entries, key)
		return nil, false
	}
	return e.value, true
}

// set stores a value with the given TTL. ttl <= 0 stores it for 30s,
// the dashboard's typical auto-refresh cadence.
func (c *aggCache) set(key string, value any, ttl time.Duration) {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = aggCacheEntry{value: value, expiresAt: time.Now().Add(ttl)}
}

// invalidate drops every entry whose key starts with the given prefix.
// Called when an event known to affect cached aggregations is recorded
// (e.g. a new login_failed should bust the failed-logins cache).
//
// Empty prefix clears the whole cache — use sparingly; it's fine on
// shutdown but a flush-everything in the hot path defeats the cache.
func (c *aggCache) invalidate(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if prefix == "" {
		c.entries = make(map[string]aggCacheEntry)
		return
	}
	for k := range c.entries {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(c.entries, k)
		}
	}
}

// memo is the canonical helper: try cache, on miss compute and store.
// Generics let the caller skip the type-assertion at call sites.
func memo[T any](c *aggCache, key string, ttl time.Duration, compute func() T) T {
	if v, ok := c.get(key); ok {
		if typed, ok := v.(T); ok {
			return typed
		}
	}
	out := compute()
	c.set(key, out, ttl)
	return out
}
