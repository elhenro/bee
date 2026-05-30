package browser

import "testing"

func TestConsoleRing_DrainReturnsAndClears(t *testing.T) {
	s := &Session{}
	s.pushConsole("log", "hello")
	s.pushConsole("error", "boom")
	got := s.drainConsole()
	if len(got) != 2 {
		t.Fatalf("want 2 msgs, got %d", len(got))
	}
	if got[0].Level != "log" || got[0].Text != "hello" {
		t.Errorf("msg0 wrong: %+v", got[0])
	}
	if len(s.drainConsole()) != 0 {
		t.Error("drain should clear the buffer")
	}
}

func TestConsoleRing_CapsAtMax(t *testing.T) {
	s := &Session{}
	for i := 0; i < consoleRingMax+50; i++ {
		s.pushConsole("log", "x")
	}
	if got := len(s.drainConsole()); got != consoleRingMax {
		t.Errorf("ring not capped: got %d want %d", got, consoleRingMax)
	}
}
