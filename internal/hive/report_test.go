package hive

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

func TestParseReport_BlockWithMultilineValues(t *testing.T) {
	final := "edited the parser.\n\n## report\nstatus: Partial\ndid: wrote foo.go\nand bar.go\nleft: tests\nnext: go test ./... fails on TestX\n  see line 12\n"
	r, ok := ParseReport(final)
	if !ok {
		t.Fatal("ParseReport: no block found")
	}
	if r.Status != ReportPartial {
		t.Errorf("Status = %q, want partial (lowercased)", r.Status)
	}
	if r.Did != "wrote foo.go\nand bar.go" {
		t.Errorf("Did = %q, continuation line lost", r.Did)
	}
	if r.Left != "tests" || !strings.HasPrefix(r.Next, "go test") || !strings.Contains(r.Next, "line 12") {
		t.Errorf("fields = %+v", r)
	}
}

func TestParseReport_NoBlock(t *testing.T) {
	if _, ok := ParseReport("all done, nothing to report"); ok {
		t.Error("ParseReport found a block in plain text")
	}
}

func TestParseReport_LastHeaderWinsAndStopsAtNextHeading(t *testing.T) {
	final := "the format is:\n## report\nstatus: done | partial | blocked\n\nreal one:\n## report\nstatus: done | partial\ndid: X\n## notes\nleft: should not be read"
	r, ok := ParseReport(final)
	if !ok {
		t.Fatal("no block")
	}
	if r.Status != ReportDone || r.Did != "X" {
		t.Errorf("got %+v, want the last block with status reduced to its first word", r)
	}
	if r.Left != "" {
		t.Errorf("Left = %q, parsing ran past the next heading", r.Left)
	}
}

func TestReport_LinePicksTheTellingField(t *testing.T) {
	cases := []struct {
		r    Report
		want string
	}{
		{Report{Status: ReportDone, Did: "wrote a\nand b", Next: "n"}, "done: wrote a"},
		{Report{Status: ReportPartial, Did: "d", Left: "tests"}, "partial: tests"},
		{Report{Status: ReportBlocked, Did: "d", Next: "needs key"}, "blocked: needs key"},
		{Report{Status: ReportDied, Cause: "max iterations", Next: "x"}, "died: max iterations"},
		{Report{}, "unreported"},
		{Report{Did: "first line"}, "unreported: first line"},
	}
	for _, c := range cases {
		if got := c.r.Line(); got != c.want {
			t.Errorf("Line(%+v) = %q, want %q", c.r, got, c.want)
		}
	}
	long := Report{Status: ReportDone, Did: strings.Repeat("x", 300)}
	if got := long.Line(); len([]rune(got)) > reportLineChars+10 {
		t.Errorf("Line not clipped: %d runes", len([]rune(got)))
	}
}

func TestReport_RenderRoundTrips(t *testing.T) {
	in := Report{Status: ReportBlocked, Did: "a", Left: "b", Next: "c\nd"}
	out, ok := ParseReport(in.Render())
	if !ok || out != in {
		t.Errorf("round trip: got %+v (ok=%v), want %+v", out, ok, in)
	}
	if strings.Contains(Report{Status: ReportDone}.Render(), "cause") {
		t.Error("Render printed an empty field")
	}
}

// reportingRunner is a Runner that can distill its own death.
type reportingRunner struct {
	runnerFunc
	report string
	err    error
	calls  int
}

func (r *reportingRunner) DeathReport(_ context.Context, _ []types.Message, _ error) (string, error) {
	r.calls++
	return r.report, r.err
}

func TestReportFor_CleanExit(t *testing.T) {
	run := loop.RunResult{FinalText: "did it\n## report\nstatus: done\ndid: shipped"}
	if got := reportFor(context.Background(), nil, run, nil); got.Status != ReportDone || got.Did != "shipped" {
		t.Errorf("parsed = %+v", got)
	}
	run = loop.RunResult{FinalText: "\n  just prose\nmore"}
	got := reportFor(context.Background(), nil, run, nil)
	if got.Status != "" || got.Did != "just prose" {
		t.Errorf("unreported fallback = %+v, want first line as did", got)
	}
}

func TestReportFor_DeathDistilledWhenRunnerCan(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleUser}}
	cause := errors.New("max iterations (3) reached")
	rr := &reportingRunner{report: "## report\nstatus: died\ndid: tried A\nnext: B"}
	got := reportFor(context.Background(), rr, loop.RunResult{Messages: msgs}, cause)
	if rr.calls != 1 {
		t.Fatalf("DeathReport calls = %d, want 1", rr.calls)
	}
	if got.Status != ReportDied || got.Did != "tried A" || got.Next != "B" {
		t.Errorf("got %+v", got)
	}
	if got.Cause != cause.Error() {
		t.Errorf("Cause = %q, want the run error filled in", got.Cause)
	}

	// distillation failed: cause alone survives.
	rr = &reportingRunner{err: errors.New("provider down")}
	got = reportFor(context.Background(), rr, loop.RunResult{Messages: msgs}, cause)
	if got.Status != ReportDied || got.Cause != cause.Error() {
		t.Errorf("fallback = %+v", got)
	}

	// no Reporter, no block in a free-text brief, dead ctx: never distill.
	plain := runnerFunc(func(context.Context, string) (loop.RunResult, error) { return loop.RunResult{}, nil })
	if got := reportFor(context.Background(), plain, loop.RunResult{Messages: msgs}, cause); got.Status != ReportDied || got.Cause == "" {
		t.Errorf("non-reporter = %+v", got)
	}
	rr = &reportingRunner{report: "free text brief"}
	if got := reportFor(context.Background(), rr, loop.RunResult{Messages: msgs}, cause); got.Next != "free text brief" {
		t.Errorf("free-text brief must land in next, got %+v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rr = &reportingRunner{report: "x"}
	_ = reportFor(ctx, rr, loop.RunResult{Messages: msgs}, cause)
	if rr.calls != 0 {
		t.Error("DeathReport called on a dead ctx")
	}
}
