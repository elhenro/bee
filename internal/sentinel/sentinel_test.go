package sentinel

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		in   string
		want Kind
	}{
		{"DONE: shipped the migration", KindDone},
		{"  DONE: trailing whitespace", KindDone},
		{"line one\nDONE: anchored", KindDone},
		{"BLOCKED: cannot infer schema", KindBlocked},
		{"NEEDS-INPUT: should I use jwt or sessions?", KindNeedsInput},
		{"working on it", KindNone},
		{"the word DONE appears mid-line", KindNone}, // not anchored
		// DONE wins over BLOCKED if both present (agent completed but quoted earlier error)
		{"BLOCKED: prior text\nDONE: actually resolved", KindDone},
		// weak/quantized models wrap the sentinel in markdown the prompt never
		// asked for — tolerate the decoration so they can still converge.
		{"**DONE:** shipped the migration", KindDone},
		{"**DONE**: colon outside the bold", KindDone},
		{"- DONE: bullet form", KindDone},
		{"> BLOCKED: quoted blocker", KindBlocked},
		{"## NEEDS-INPUT: which auth?", KindNeedsInput},
		{"`DONE: fenced inline`", KindDone},
		// decoration must not manufacture a sentinel from prose
		{"- almost DONE: but not yet", KindNone}, // keyword still mid-line
		{"summary of work done: see above", KindNone},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := Classify(tc.in); got != tc.want {
				t.Errorf("Classify(%q)=%v want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestKindString(t *testing.T) {
	if KindDone.String() != "DONE" {
		t.Errorf("KindDone=%q", KindDone.String())
	}
	if KindNone.String() != "" {
		t.Errorf("KindNone=%q want empty", KindNone.String())
	}
}
