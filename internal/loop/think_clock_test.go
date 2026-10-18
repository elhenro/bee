package loop

import (
	"testing"
	"time"
)

func TestThinkClock(t *testing.T) {
	var c thinkClock
	if c.ms() != 0 {
		t.Fatal("no reasoning should read 0")
	}
	c.reasoning()
	if c.ms() != 1 {
		t.Fatalf("a burst of reasoning should count as at least 1ms, got %d", c.ms())
	}
	time.Sleep(20 * time.Millisecond)
	c.reasoning()
	c.answered()
	frozen := c.ms()
	if frozen < 20 {
		t.Fatalf("clock should cover first to last delta, got %dms", frozen)
	}
	time.Sleep(10 * time.Millisecond)
	c.reasoning() // interleaved reasoning after the answer began must not stretch it
	c.answered()
	if c.ms() != frozen {
		t.Fatalf("closed clock moved: %d -> %d", frozen, c.ms())
	}
}

func TestThinkClock_AnswerWithoutReasoning(t *testing.T) {
	var c thinkClock
	c.answered()
	if c.ms() != 0 {
		t.Fatalf("answer-only turn should have no thinking time, got %d", c.ms())
	}
}
