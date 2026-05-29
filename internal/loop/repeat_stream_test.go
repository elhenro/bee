package loop

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

// loopingProvider emits the same text delta forever, never sending EventDone —
// simulating a small model wedged in a token loop. Respects ctx cancellation so
// the watchdog can cut it.
type loopingProvider struct{}

func (p *loopingProvider) Name() string { return "looping" }
func (p *loopingProvider) Stream(ctx context.Context, _ llm.Request) (<-chan llm.Event, error) {
	ch := make(chan llm.Event)
	go func() {
		defer close(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case ch <- llm.Event{Type: llm.EventTextDelta, Delta: "I will write the report.\n"}:
			}
		}
	}()
	return ch, nil
}

// a wedged repetition loop must be cut and, after loopCutBailAt consecutive
// cuts, bail with ErrRepeatStream rather than hanging until ctx timeout.
func TestStream_CutsRepetitionLoopAndBails(t *testing.T) {
	cfg := config.Defaults()
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
	cfg.Mode = "edit" // skip auto-classify (would call the looping provider)
	eng := &Engine{
		Provider: &loopingProvider{},
		Tools:    tools.NewRegistry(),
		Memory:   stubMemStore{},
		Cfg:      cfg,
		Cwd:      ".",
		Stdout:   io.Discard,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := eng.Run(ctx, "write a report")
	if !errors.Is(err, ErrRepeatStream) {
		t.Fatalf("expected ErrRepeatStream, got %v", err)
	}
}

func TestDegenerateTailPeriod_DetectsLoopedLines(t *testing.T) {
	// the real symptom: a small model cycling the same 3 lines forever.
	unit := "One detail: The user's prompt is \"paul schober\".\nI will write the report.\nI will write the report.\n"
	s := "ok, here is my plan.\n" + strings.Repeat(unit, 20)
	if p := degenerateTailPeriod(s); p == 0 {
		t.Fatalf("expected a loop period, got 0")
	}
}

func TestDegenerateTailPeriod_SinglePhraseLoop(t *testing.T) {
	s := strings.Repeat("I will write the report. ", 30)
	if p := degenerateTailPeriod(s); p == 0 {
		t.Fatal("expected detection of single-phrase loop")
	}
}

func TestDegenerateTailPeriod_IgnoresLegitText(t *testing.T) {
	cases := []string{
		"",
		"a short answer with no repetition at all.",
		"function foo() {\n  return bar(baz);\n}\n", // distinct lines
		strings.Repeat("ab", 50),                    // unit < loopMinUnit
		"item 1\nitem 2\nitem 3\nitem 4\nitem 5\n",  // varied lines
	}
	for _, c := range cases {
		if p := degenerateTailPeriod(c); p != 0 {
			t.Fatalf("false positive period=%d on %q", p, c)
		}
	}
}

func TestDegenerateTailPeriod_NeedsEnoughReps(t *testing.T) {
	// fewer than loopMinReps repeats must not trip.
	s := strings.Repeat("the same line here\n", loopMinReps-1)
	if p := degenerateTailPeriod(s); p != 0 {
		t.Fatalf("expected no detection below %d reps, got period=%d", loopMinReps, p)
	}
}

func TestTrimLoopedTail_CollapsesRepetition(t *testing.T) {
	unit := "loop line\n"
	s := "intro text\n" + strings.Repeat(unit, 30)
	p := degenerateTailPeriod(s)
	if p == 0 {
		t.Fatal("setup: expected a detected period")
	}
	got := trimLoopedTail(s, p)
	if len(got) >= len(s) {
		t.Fatalf("expected trim to shrink output: got %d, was %d", len(got), len(s))
	}
	if !strings.Contains(got, "intro text") {
		t.Fatal("trim dropped the non-looped prefix")
	}
	if !strings.Contains(got, "truncated") {
		t.Fatal("trim did not append the truncation marker")
	}
	// running the detector again on the trimmed text must not re-trip.
	if degenerateTailPeriod(got) != 0 {
		t.Fatal("trimmed output still reads as a loop")
	}
}

func TestTrimLoopedTail_NoopOnZeroPeriod(t *testing.T) {
	s := "no loop here"
	if got := trimLoopedTail(s, 0); got != s {
		t.Fatalf("expected unchanged, got %q", got)
	}
}
