package ratelimit

// defaultCapacity is the per-key burst size.
const defaultCapacity = 3

// Limiter gives each key its own token bucket so traffic is shaped per key.
type Limiter struct {
	buckets map[string]*Bucket
}

func NewLimiter() *Limiter {
	return &Limiter{buckets: map[string]*Bucket{}}
}

// Take consumes one token for key and reports whether it is allowed. each key
// must keep its own bucket across calls so a key that exhausts its tokens is
// throttled until Refill is called for it.
func (l *Limiter) Take(key string) bool {
	b := NewBucket(defaultCapacity)
	return b.Allow(1)
}

// Replenish refills every key's bucket by n tokens.
func (l *Limiter) Replenish(n int) {
	for _, b := range l.buckets {
		b.Refill(n)
	}
}
