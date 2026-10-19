package caveman

import (
	"testing"
)

func TestRulesNonEmptyForActiveLevels(t *testing.T) {
	for _, lvl := range []Level{Lite, Full, Ultra} {
		r := Rules(lvl)
		if r == "" {
			t.Errorf("Rules(%q) returned empty", lvl)
		}
		if len(r) > 4096 {
			t.Errorf("Rules(%q) is %d bytes, want ≤ 4KB", lvl, len(r))
		}
	}
}

func TestRulesOffEmpty(t *testing.T) {
	if got := Rules(Off); got != "" {
		t.Errorf("Rules(Off) = %q, want \"\"", got)
	}
}

func TestRulesUnknownEmpty(t *testing.T) {
	if got := Rules(Level("bogus")); got != "" {
		t.Errorf("Rules(bogus) = %q, want \"\"", got)
	}
}

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in   string
		want Level
		err  bool
	}{
		{"", Default, false},
		{"off", Off, false},
		{"OFF", Off, false},
		{"none", Off, false},
		{"lite", Lite, false},
		{"  light  ", Lite, false},
		{"full", Full, false},
		{"default", Full, false},
		{"ultra", Ultra, false},
		{"max", Ultra, false},
		{"bogus", "", true},
		{"123", "", true},
	}
	for _, c := range cases {
		got, err := ParseLevel(c.in)
		if c.err {
			if err == nil {
				t.Errorf("ParseLevel(%q) want error, got nil", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseLevel(%q) unexpected error %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("ParseLevel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDefaultIsFull(t *testing.T) {
	if Default != Full {
		t.Errorf("Default = %q, want Full", Default)
	}
}
