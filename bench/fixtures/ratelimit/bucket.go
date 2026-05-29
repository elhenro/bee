package ratelimit

// Bucket is a token bucket. tokens are consumed by Allow and replenished by
// Refill. capacity caps how many tokens can accumulate.
type Bucket struct {
	tokens   int
	capacity int
}

// NewBucket starts full at capacity.
func NewBucket(capacity int) *Bucket {
	return &Bucket{tokens: capacity, capacity: capacity}
}

// Allow consumes n tokens and reports whether the request is permitted.
// it must never let tokens go negative: if fewer than n are available it
// consumes nothing and returns false.
func (b *Bucket) Allow(n int) bool {
	if b.tokens < n {
		return false
	}
	b.tokens -= n
	return true
}

// Refill adds n tokens but never beyond capacity.
func (b *Bucket) Refill(n int) {
	b.tokens += n
	if b.tokens > b.capacity {
		b.tokens = b.capacity
	}
}
