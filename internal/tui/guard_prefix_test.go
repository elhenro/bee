package tui

import "testing"

func TestStripGuardPrefixes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"none", "exit 0\nok", "exit 0\nok"},
		{"repeat", "[repeat] same call to bash fired 3x — try a different approach.\n\nexit 0", "exit 0"},
		{"iter args", "[iter 3/6] half the budget spent.\n\nreal output", "real output"},
		{"context args", "[context at 80%] summarize.\n\nbody", "body"},
		{"stall args", "[stall 12 iters] previous nudge ignored.\n\nbody", "body"},
		{"stacked", "[repeat] a.\n\n[verify] b.\n\nbody", "body"},
		{"real bracket kept", "[INFO] starting up\nline2", "[INFO] starting up\nline2"},
		{"json array kept", "[1, 2, 3]", "[1, 2, 3]"},
		{"warning only", "[repeat] nothing after.\n\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripGuardPrefixes(c.in); got != c.want {
				t.Errorf("stripGuardPrefixes(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
