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
