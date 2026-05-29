package store

// build returns a ready Cache.
func build() *Cache {
	return NewCache()
}

var _ = build
