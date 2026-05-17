// Result fan-in helpers. Plain functions over []Result so callers can
// compose them without bringing in the Pool dependency.
package hive

import (
	"fmt"
	"strings"
	"time"
)

// Collect drains ch into a slice. Returns when the channel is closed.
// Order matches arrival order, not submission order.
func Collect(ch <-chan Result) []Result {
	out := make([]Result, 0, 8)
	for r := range ch {
		out = append(out, r)
	}
	return out
}

// Summary renders a human-readable summary block: counts, wall-clock,
// and one line per worker. Wall-clock is min(Started)..max(Ended), so it
// reflects actual parallel duration, not sum-of-bee-times.
func Summary(results []Result) string {
	if len(results) == 0 {
		return "hive: no workers ran\n"
	}
	var ok, fail int
	var earliest, latest time.Time
	for _, r := range results {
		if r.Err == nil {
			ok++
		} else {
			fail++
		}
		if earliest.IsZero() || r.Started.Before(earliest) {
			earliest = r.Started
		}
		if r.Ended.After(latest) {
			latest = r.Ended
		}
	}
	wall := latest.Sub(earliest)
	if wall < 0 {
		wall = 0
	}

	var b strings.Builder
	fmt.Fprintf(&b, "hive: %d ok, %d failed, %s wall\n", ok, fail, wall.Round(time.Millisecond))
	for _, r := range results {
		tag := "ok"
		final := oneLine(r.Final)
		if r.Err != nil {
			tag = "err"
			final = r.Err.Error()
		}
		dur := r.Ended.Sub(r.Started).Round(time.Millisecond)
		fmt.Fprintf(&b, "[%s] %s (%s): %s\n", tag, r.Name, dur, final)
	}
	return b.String()
}

// MergeText concatenates the final text from each successful worker into
// a single transcript with `## <name>` headers. Failed workers are omitted.
func MergeText(results []Result) string {
	var b strings.Builder
	first := true
	for _, r := range results {
		if r.Err != nil {
			continue
		}
		if !first {
			b.WriteString("\n")
		}
		first = false
		fmt.Fprintf(&b, "## %s\n\n", r.Name)
		b.WriteString(strings.TrimRight(r.Final, "\n"))
		b.WriteString("\n")
	}
	return b.String()
}

// oneLine collapses Final to a single line for the summary table. Long
// outputs are truncated to keep the summary scannable.
func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(no output)"
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	const max = 120
	if len(s) > max {
		s = s[:max-1] + "…"
	}
	return s
}
