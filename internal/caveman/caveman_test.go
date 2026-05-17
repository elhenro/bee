package caveman

import (
	"strings"
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

func TestInjectOffReturnsInputUnchanged(t *testing.T) {
	const sys = "you are a coding agent"
	if got := Inject(sys, Off); got != sys {
		t.Errorf("Inject Off changed prompt: got %q want %q", got, sys)
	}
}

func TestInjectPrepends(t *testing.T) {
	const sys = "you are a coding agent"
	got := Inject(sys, Full)
	if !strings.HasSuffix(got, sys) {
		t.Errorf("Inject did not preserve trailing system prompt, got %q", got)
	}
	if !strings.Contains(got, "Respond terse") {
		t.Errorf("Inject did not include caveman rules, got %q", got)
	}
	if len(got) <= len(sys) {
		t.Errorf("Inject did not grow prompt: len(got)=%d len(sys)=%d", len(got), len(sys))
	}
}

func TestInjectEmptySystem(t *testing.T) {
	got := Inject("", Full)
	if got == "" {
		t.Error("Inject(empty, Full) returned empty")
	}
}

// Inject is defined as "prepend each time". Two calls yields rules twice;
// caller is responsible for not double-injecting. Lock the documented behavior.
func TestInjectPrependsEachCall(t *testing.T) {
	const sys = "X"
	once := Inject(sys, Full)
	twice := Inject(once, Full)
	if len(twice) <= len(once) {
		t.Errorf("second Inject did not prepend again: once=%d twice=%d", len(once), len(twice))
	}
	if strings.Count(twice, "Respond terse") != 2 {
		t.Errorf("expected 2 caveman blocks after double-inject, got %d", strings.Count(twice, "Respond terse"))
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
