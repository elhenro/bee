package knowledge

import (
	"strings"
	"testing"
	"time"
)

func TestDaysSinceClamps(t *testing.T) {
	now := time.Now()
	if got := daysSince(now.Add(48*time.Hour), now); got != 0 {
		t.Fatalf("future should clamp to 0, got %d", got)
	}
	if got := daysSince(now.Add(-90*time.Minute), now); got != 0 {
		t.Fatalf("90min ago is 0, got %d", got)
	}
	if got := daysSince(now.Add(-25*time.Hour), now); got != 1 {
		t.Fatalf("25h ago is 1, got %d", got)
	}
	if got := daysSince(now.Add(-(72*time.Hour + time.Hour)), now); got != 3 {
		t.Fatalf("3d+1h ago should be 3, got %d", got)
	}
}

func TestAgeSinceWords(t *testing.T) {
	now := time.Now()
	cases := []struct {
		t    time.Time
		want string
	}{
		{now.Add(-time.Hour), "today"},
		{now.Add(-25 * time.Hour), "yesterday"},
		{now.Add(-72 * time.Hour), "3 days ago"},
	}
	for _, c := range cases {
		if got := ageSinceAt(c.t, now); got != c.want {
			t.Errorf("ageSinceAt %v: want %q got %q", c.t, c.want, got)
		}
	}
}

func TestStalenessNoteFiresOnExpired(t *testing.T) {
	now := time.Now()
	if got := stalenessNoteAt(time.Time{}, now); got != "" {
		t.Fatalf("zero expires should be silent, got %q", got)
	}
	if got := stalenessNoteAt(now.Add(24*time.Hour), now); got != "" {
		t.Fatalf("future expiry should be silent, got %q", got)
	}
	if got := stalenessNoteAt(now.Add(-72*time.Hour), now); !strings.Contains(got, "expired") {
		t.Fatalf("want 'expired' phrase, got %q", got)
	}
	if got := stalenessNoteAt(now.Add(-72*time.Hour), now); !strings.Contains(got, "3") {
		t.Fatalf("want day count, got %q", got)
	}
}
