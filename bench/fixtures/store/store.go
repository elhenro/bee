package store

// Cache holds string entries.
type Cache struct {
	entries map[string]string
}

// NewCache builds an empty Cache.
func NewCache() *Cache {
	return &Cache{entries: map[string]string{}}
}

// Get returns the value stored for key.
func (c *Cache) Get(key string) string {
	return c.entries[key]
}
