package web_fetch

import (
	"sync"
	"time"
)

// CacheEntry stores fetched content with metadata
type CacheEntry struct {
	Bytes       int
	Code        int
	CodeText    string
	ContentType string
	Content     string
	ExpiresAt   time.Time
}

// Cache implements a simple LRU cache with TTL
type Cache struct {
	mu     sync.RWMutex
	items  map[string]*CacheEntry
	order  []string
	maxLen int
	ttl    time.Duration
}

// NewCache creates a new cache with the given TTL and max length
func NewCache(ttl time.Duration, maxLen int) *Cache {
	return &Cache{items: make(map[string]*CacheEntry), maxLen: maxLen, ttl: ttl}
}

// Get retrieves an entry from the cache
func (c *Cache) Get(key string) (*CacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.items[key]
	if !exists || time.Now().After(entry.ExpiresAt) {
		return nil, false
	}
	return entry, true
}

// Set adds an entry to the cache
func (c *Cache) Set(key string, entry *CacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If key already exists, update it
	if _, exists := c.items[key]; exists {
		c.items[key] = entry
		return
	}

	// If cache is full, remove the oldest entry
	if len(c.order) >= c.maxLen {
		delete(c.items, c.order[0])
		c.order = c.order[1:]
	}

	c.items[key] = entry
	c.order = append(c.order, key)
}

var cache = NewCache(15*time.Minute, 50) // 15min TTL, 50-entry LRU
