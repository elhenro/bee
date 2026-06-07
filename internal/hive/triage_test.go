package hive

import "testing"

func TestParseTriageSimple(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"simple", true},
		{"Simple.", true},
		{"  SIMPLE\n", true},
		{`"simple"`, true},
		{"complex", false},
		{"complex — multi-file", false},
		{"", false},
		{"I think this is simple", false}, // must be a prefix, not buried
		{"simpler", true},                 // prefix match; acceptable
	}
	for _, c := range cases {
		if got := parseTriageSimple(c.in); got != c.want {
			t.Errorf("parseTriageSimple(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestTriageSimple_NilProviderIsComplex(t *testing.T) {
	if TriageSimple(nil, nil, "m", "task") {
		t.Error("nil provider must default to complex (false)")
	}
}

func TestTriageSimple_EmptyTaskIsComplex(t *testing.T) {
	// empty task short-circuits to complex before any provider call
	if TriageSimple(nil, nil, "m", "   ") {
		t.Error("empty task must default to complex (false)")
	}
}
