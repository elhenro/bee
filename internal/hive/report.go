package hive

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

// Report is a worker's exit brief. The hive contract (pheromone.go) asks every
// bee to end its final message with a `## report` block (status / did / left /
// next) so the queen synthesizes from honest reports, not from guesses about
// silent work. When a worker dies first (iteration cap, escalate, stream
// failure) the harness builds the report from its transcript instead: death is
// information and must not evaporate with the bee.
type Report struct {
	Status string // done | partial | blocked | died | "" when the worker never reported
	Cause  string // death cause when Status == died
	Did    string
	Left   string
	Next   string
}

// Status values a worker may declare; died is harness-assigned.
const (
	ReportDone    = "done"
	ReportPartial = "partial"
	ReportBlocked = "blocked"
	ReportDied    = "died"
)

// reportHeader opens the block. Matched case-insensitively on its own line.
const reportHeader = "## report"

// reportLineChars caps the one-line view a sibling sees on the pheromone card.
const reportLineChars = 200

// deathReportTimeout bounds the distillation call: a provider that just failed
// the worker must not hang the whole hive on the way out.
const deathReportTimeout = 60 * time.Second

// Reporter is a Runner that can distill its own transcript into a death report
// when a run dies. loop.Engine satisfies it; runner wrappers forward it. A
// Runner without it (test stubs) dies with the cause alone.
type Reporter interface {
	DeathReport(ctx context.Context, msgs []types.Message, cause error) (string, error)
}

// ParseReport extracts the `## report` block from a worker's final text. The
// block runs from the header to the end of the text or the next `## ` heading;
// each `key: value` line starts a field and bare lines continue the previous
// one, so multi-line values survive. The last header wins so a quoted template
// earlier in the answer does not shadow the real block. ok is false when no
// block is present.
func ParseReport(final string) (Report, bool) {
	lines := strings.Split(final, "\n")
	start := -1
	for i, l := range lines {
		if strings.EqualFold(strings.TrimSpace(l), reportHeader) {
			start = i
		}
	}
	if start < 0 {
		return Report{}, false
	}
	var r Report
	var cur *string
	for _, l := range lines[start+1:] {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "## ") {
			break
		}
		if f := r.field(t); f != nil {
			cur = f
			continue
		}
		if cur != nil && t != "" {
			*cur = strings.TrimSpace(*cur + "\n" + t)
		}
	}
	// first token only: "done | partial | blocked" pasted from the template
	// still yields a status word rather than the whole menu.
	if fields := strings.Fields(strings.ToLower(r.Status)); len(fields) > 0 {
		r.Status = strings.Trim(fields[0], ".,;")
	}
	return r, true
}

// field recognizes a `key: value` line, stores the value and returns the field
// so continuation lines can append to it. nil for any other line.
func (r *Report) field(line string) *string {
	key, val, ok := strings.Cut(line, ":")
	if !ok {
		return nil
	}
	var f *string
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "status":
		f = &r.Status
	case "cause":
		f = &r.Cause
	case "did":
		f = &r.Did
	case "left":
		f = &r.Left
	case "next":
		f = &r.Next
	default:
		return nil
	}
	*f = strings.TrimSpace(val)
	return f
}

// Line is the one-line view a sibling sees on the pheromone card: the status
// plus the field that matters most for it. done shows what was done, partial
// what is left, blocked what the next bee needs, died the cause.
func (r Report) Line() string {
	status := cmp.Or(r.Status, "unreported")
	var detail string
	switch status {
	case ReportPartial:
		detail = firstNonEmpty(r.Left, r.Did)
	case ReportBlocked:
		detail = firstNonEmpty(r.Next, r.Did)
	case ReportDied:
		detail = firstNonEmpty(r.Cause, r.Next)
	default:
		detail = firstNonEmpty(r.Did, r.Next)
	}
	if detail == "" {
		return status
	}
	return status + ": " + clip(oneLineOf(detail), reportLineChars)
}

// Render writes the block back out in contract shape, for the synthesizer and
// the mirrored pheromone file. Empty fields are omitted.
func (r Report) Render() string {
	var b strings.Builder
	b.WriteString(reportHeader + "\n")
	status := cmp.Or(r.Status, "unreported")
	for _, kv := range [...][2]string{{"status", status}, {"cause", r.Cause}, {"did", r.Did}, {"left", r.Left}, {"next", r.Next}} {
		if kv[1] != "" {
			fmt.Fprintf(&b, "%s: %s\n", kv[0], kv[1])
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// reportFor builds a Result's Report after a worker run. A clean exit parses
// the block from the final text; a missing block yields an unreported Report
// carrying the answer's first line. A dead run asks the runner to distill its
// transcript; when it cannot (no Reporter, provider down, parent ctx gone) the
// report carries the cause alone, which still beats an empty Final.
func reportFor(ctx context.Context, r Runner, run loop.RunResult, err error) Report {
	if err == nil {
		if rep, ok := ParseReport(run.FinalText); ok {
			return rep
		}
		var rep Report
		if t := strings.TrimSpace(run.FinalText); t != "" {
			rep.Did = oneLineOf(t)
		}
		return rep
	}
	rep := Report{Status: ReportDied, Cause: err.Error()}
	rp, ok := r.(Reporter)
	if !ok || ctx.Err() != nil || len(run.Messages) == 0 {
		return rep
	}
	dctx, cancel := context.WithTimeout(ctx, deathReportTimeout)
	defer cancel()
	text, derr := rp.DeathReport(dctx, run.Messages, err)
	if derr != nil {
		return rep
	}
	if parsed, ok := ParseReport(text); ok {
		if parsed.Status == "" {
			parsed.Status = ReportDied
		}
		if parsed.Cause == "" {
			parsed.Cause = rep.Cause
		}
		return parsed
	}
	rep.Next = strings.TrimSpace(text)
	return rep
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// oneLineOf keeps the first non-empty line of s.
func oneLineOf(s string) string {
	for l := range strings.SplitSeq(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return ""
}

// clip cuts s to max runes with a bare ellipsis. Rune-safe so the cut never
// splits a multi-byte character.
func clip(s string, max int) string {
	count := 0
	for i := range s {
		if count == max {
			return s[:i] + "…"
		}
		count++
	}
	return s
}
