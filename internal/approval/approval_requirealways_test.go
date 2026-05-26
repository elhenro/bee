package approval

import (
	"context"
	"testing"
)

// requireAlways keys re-prompt every call even after AllowSession was granted.
func TestCache_RequireAlways_BypassesSessionCache(t *testing.T) {
	rec := &recordingApprover{verdict: AllowSession}
	c := NewCacheWithRequire(rec, nil, []string{"git-push-force"}, nil)

	for i := 0; i < 3; i++ {
		d, _ := c.Request(context.Background(), "git push --force", "git-push-force", "")
		if d != AllowSession {
			t.Fatalf("iter %d: got %v", i, d)
		}
	}
	if rec.calls != 3 {
		t.Fatalf("inner called %d times; requireAlways must re-prompt every call, want 3", rec.calls)
	}
}

// requireAlways does NOT override a persistent AllowAlways grant — the user
// can still opt into trusting a destructive key permanently.
func TestCache_RequireAlways_PersistentStillWins(t *testing.T) {
	rec := &recordingApprover{verdict: Deny}
	c := NewCacheWithRequire(rec, []string{"rm-recursive"}, []string{"rm-recursive"}, nil)

	d, _ := c.Request(context.Background(), "rm -rf x", "rm-recursive", "")
	if d != AllowAlways {
		t.Fatalf("persistent grant must win even when requireAlways is set; got %v", d)
	}
	if rec.calls != 0 {
		t.Fatalf("inner must not be called when persistent grant exists; got %d calls", rec.calls)
	}
}

// keys not in requireAlways still cache normally.
func TestCache_RequireAlways_OtherKeysCache(t *testing.T) {
	rec := &recordingApprover{verdict: AllowSession}
	c := NewCacheWithRequire(rec, nil, []string{"git-push-force"}, nil)

	for i := 0; i < 3; i++ {
		d, _ := c.Request(context.Background(), "kill -9", "kill-9", "")
		if d != AllowSession {
			t.Fatalf("iter %d: got %v", i, d)
		}
	}
	if rec.calls != 1 {
		t.Fatalf("non-requireAlways key must cache; inner calls=%d, want 1", rec.calls)
	}
}
