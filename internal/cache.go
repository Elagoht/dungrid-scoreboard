package internal

import (
	"sync"
	"time"
)

type cacheValue struct {
	entries    []ScoreEntry
	expiresAt  time.Time
}

// Cache holds in-memory top-N result sets to avoid repeated DB queries.
// It is safe for concurrent use.
type Cache struct {
	mu    sync.RWMutex
	items map[int]*cacheValue
	ttl   time.Duration
}

// NewCache creates a new Cache with the given TTL.
func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		items: make(map[int]*cacheValue),
		ttl:   ttl,
	}
}

// Get returns cached top-N entries if present and not expired.
// The boolean is true on a valid cache hit.
func (c *Cache) Get(n int) ([]ScoreEntry, bool) {
	c.mu.RLock()
	v, ok := c.items[n]
	c.mu.RUnlock()

	if !ok {
		return nil, false
	}

	if time.Now().After(v.expiresAt) {
		return nil, false
	}

	return v.entries, true
}

// Set stores top-N entries in the cache with the configured TTL.
func (c *Cache) Set(n int, entries []ScoreEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[n] = &cacheValue{
		entries:   entries,
		expiresAt: time.Now().Add(c.ttl),
	}
}

// Invalidate clears all cached entries. Called after a new score is inserted.
func (c *Cache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[int]*cacheValue)
}
