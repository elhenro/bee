package loop

import (
	"context"
	"errors"
	"fmt"
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

func TestDegenerateLowVocabTail_DetectsNoisyPhraseLoop(t *testing.T) {
	// the real symptom degenerateTailPeriod misses: two phrases shuffled in
	// irregular order, so the tail isn't byte-periodic but the vocabulary is
	// tiny. (from a real wedged-model log.)
	phrases := []string{"Writing the game file... ", "Building the game file... "}
	var b strings.Builder
	b.WriteString("Here is my detailed plan for building the terrain system with chunked noise sampling and object placement before anything else.\n")
	for i := 0; i < 80; i++ {
		// thue-morse parity: aperiodic order, so the tail is never byte-periodic.
		parity := 0
		for v := i; v > 0; v >>= 1 {
			parity ^= v & 1
		}
		b.WriteString(phrases[parity])
	}
	s := b.String()
	if degenerateTailPeriod(s) != 0 {
		t.Fatal("setup: exact-period detector should NOT catch this noisy loop")
	}
	off := degenerateLowVocabTail(s)
	if off < 0 {
		t.Fatal("expected low-vocab loop detection")
	}
	// the loop must be cut, but the meaningful preamble (which has rich, varied
	// vocab the loop words can't extend through) must survive.
	if off == 0 || !strings.Contains(s[:off], "Here is my detailed plan") {
		t.Fatalf("trim offset %d ate the real prefix", off)
	}
}

func TestDegenerateLowVocabTail_IgnoresVariedProse(t *testing.T) {
	cases := []string{
		"",
		"a short answer with no repetition at all.",
		// long but genuinely varied prose: many distinct words.
		strings.Repeat("the quick brown fox jumps over a lazy dog near the river bank today ", 8),
		// a real numbered list — distinct tokens per line.
		func() string {
			var b strings.Builder
			for i := 0; i < 90; i++ {
				fmt.Fprintf(&b, "item number %d here\n", i)
			}
			return b.String()
		}(),
	}
	for _, c := range cases {
		if off := degenerateLowVocabTail(c); off != -1 {
			t.Fatalf("false positive offset=%d on %q", off, c[:min(40, len(c))])
		}
	}
}

func TestTrimLoopedTailAt_Collapses(t *testing.T) {
	s := "intro\n" + strings.Repeat("loop ", 100)
	got := trimLoopedTailAt(s, len("intro\n"))
	if !strings.Contains(got, "intro") || !strings.Contains(got, "truncated") {
		t.Fatalf("trim should keep prefix + marker, got %q", got)
	}
	if len(got) >= len(s) {
		t.Fatal("expected trim to shrink output")
	}
	if got := trimLoopedTailAt("x", -1); got != "x" {
		t.Fatalf("negative offset must be a noop, got %q", got)
	}
}
