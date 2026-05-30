package loop

import "testing"

func TestScaledCompactThreshold(t *testing.T) {
	const big = 131072 // 128k window

	cases := []struct {
		name   string
		base   float64
		budget int
		want   float64
	}{
		// explicit low value: user wants early compaction, honored verbatim
		{"low honored on big window", 0.2, big, 0.2},
		{"low honored on small window", 0.2, 32768, 0.2},
		// default widens on big window to avoid compacting too early
		{"default widens on big window", 0.75, big, 1.0 - 8000.0/float64(big)},
		// default unchanged when window too small to widen past it
		{"default unchanged on small window", 0.75, 16000, 0.75},
		// disabled cases pass through
		{"budget zero", 0.2, 0, 0.2},
		{"base zero", 0.0, big, 0.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := scaledCompactThreshold(c.base, c.budget)
			if got != c.want {
				t.Errorf("scaledCompactThreshold(%v, %d) = %v, want %v", c.base, c.budget, got, c.want)
			}
		})
	}
}
