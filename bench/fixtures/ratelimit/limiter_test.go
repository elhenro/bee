package ratelimit

import "testing"

func TestLimiterThrottles(t *testing.T) {
	l := NewLimiter()
	// capacity is 3, so the first three takes pass and the fourth is throttled.
	for i := 0; i < 3; i++ {
		if !l.Take("a") {
			t.Fatalf("take %d should be allowed", i)
		}
	}
	if l.Take("a") {
		t.Fatalf("fourth take should be throttled")
	}
}
