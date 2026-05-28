package goal

import (
	"strings"
	"testing"
	"time"
)

func TestSetClearTick(t *testing.T) {
	var s State
	now := time.Now()
	s.Set("make tests pass", 100, now)
	if !s.Active {
		t.Fatal("want Active after Set")
	}
	if s.Condition != "make tests pass" {
		t.Fatalf("condition = %q", s.Condition)
	}
	if s.Turns != 0 {
		t.Fatalf("turns = %d, want 0", s.Turns)
	}
	if s.TokenBaseline != 100 {
		t.Fatalf("baseline = %d, want 100", s.TokenBaseline)
	}
	s.Tick()
	s.Tick()
	if s.Turns != 2 {
		t.Fatalf("turns = %d, want 2", s.Turns)
	}
	s.Clear()
	if s.Active {
		t.Fatal("want inactive after Clear")
	}
	if s.Turns != 0 {
		t.Fatalf("turns = %d after Clear, want 0", s.Turns)
	}
}

func TestCapsExceeded(t *testing.T) {
	var s State
	s.Set("x", 0, time.Now())
	s.Caps = Caps{MaxTurns: 3}
	for i := 0; i < 3; i++ {
		s.Tick()
	}
	exceeded, reason := s.CapsExceeded(0)
	if !exceeded || reason == "" {
		t.Fatalf("turn cap: exceeded=%v reason=%q", exceeded, reason)
	}

	var s2 State
	s2.Set("y", 0, time.Now())
	s2.Caps = Caps{MaxTokens: 1000}
	exceeded, reason = s2.CapsExceeded(1500)
	if !exceeded || reason == "" {
		t.Fatalf("token cap: exceeded=%v reason=%q", exceeded, reason)
	}
	if s2.TokensSpent(1500) != 1500 {
		t.Fatalf("spent = %d, want 1500", s2.TokensSpent(1500))
	}

	var s3 State
	s3.Set("z", 0, time.Now())
	s3.Caps = Caps{MaxTurns: 5, MaxTokens: 1000}
	if ex, _ := s3.CapsExceeded(500); ex {
		t.Fatal("should be within bounds")
	}
}

func TestIsClearWord(t *testing.T) {
	cases := map[string]bool{
		"clear": true, "stop": true, "off": true,
		"reset": true, "none": true, "cancel": true,
		"CLEAR": true, " stop ": true,
		"foo": false, "": false, "go": false,
	}
	for in, want := range cases {
		if got := IsClearWord(in); got != want {
			t.Errorf("IsClearWord(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestStatusAndStatLine(t *testing.T) {
	var s State
	now := time.Now()
	s.Set("ship the feature", 0, now)
	s.Tick()
	s.LastReason = "still building"

	status := s.Status(5000, now.Add(time.Minute))
	if status == "" || !strings.Contains(status, "ship the feature") {
		t.Fatalf("status missing condition: %q", status)
	}
	if !strings.Contains(status, "still building") {
		t.Fatalf("status missing reason: %q", status)
	}

	line := s.StatLine(5000)
	if line == "" || !strings.Contains(line, "ship the feature") {
		t.Fatalf("statline missing condition: %q", line)
	}

	var empty State
	if empty.StatLine(0) != "" {
		t.Fatal("inactive StatLine should be empty")
	}
}
