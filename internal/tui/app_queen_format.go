package tui

import (
	"fmt"
	"strings"

	"github.com/elhenro/bee/internal/hive"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

// reviewCards accumulates one dimension's findings and emits them as a single
// card when the next dimension starts (or the run ends).
type reviewCards struct {
	emit     func(string)
	dim      string
	findings []hive.Finding
	seen     bool
}

func (c *reviewCards) start(dim string) { c.dim, c.seen = dim, true }

func (c *reviewCards) add(f hive.Finding) { c.findings = append(c.findings, f) }

func (c *reviewCards) flush() {
	if !c.seen {
		return
	}
	c.emit(formatReview(c.dim, c.findings))
	c.seen = false
	c.findings = c.findings[:0]
}

// hiveTaskFromContent flattens the user's content blocks into the string task
// the hive Runner consumes. All non-empty text blocks are joined so nothing is
// dropped. The Runner interface is string-only, so images can't reach workers;
// rather than drop them silently, append a note recording how many were
// attached so sub-agents (and the user) know they exist but aren't visible.
func hiveTaskFromContent(blocks []types.ContentBlock) string {
	var texts []string
	images := 0
	for _, b := range blocks {
		switch b.Type {
		case types.BlockText:
			if t := strings.TrimSpace(b.Text); t != "" {
				texts = append(texts, t)
			}
		case types.BlockImage:
			images++
		}
	}
	task := strings.Join(texts, "\n\n")
	if images > 0 {
		task += fmt.Sprintf("\n\n[note: %d image(s) were attached but are not visible to sub-agents]", images)
	}
	return strings.TrimSpace(task)
}

// formatPlan renders the queen's decomposition as a numbered, role-tagged list.
func formatPlan(plan []hive.SubTask) string {
	var b strings.Builder
	b.WriteString("## ⬢ queen — plan\n\n")
	for i, st := range plan {
		role := st.Role
		if role == "" {
			role = "builder"
		}
		fmt.Fprintf(&b, "%d. **%s** — %s\n", i+1, role, strings.TrimSpace(st.Task))
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatReview renders one dimension's review card: each finding with its
// re-verification verdict (✓ confirmed / ✗ refuted-and-dropped), or a clean
// note when the dimension turned up nothing. Shows the user every check ran.
func formatReview(dim string, findings []hive.Finding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### ⬢ review — %s\n\n", dim)
	if len(findings) == 0 {
		b.WriteString("_clean — no findings_")
		return b.String()
	}
	for _, f := range findings {
		mark, tail := "✓", ""
		if !f.Confirmed {
			mark, tail = "✗", " — _dropped_"
		}
		fmt.Fprintf(&b, "- %s %s", mark, strings.TrimSpace(f.Claim))
		if v := strings.TrimSpace(f.Verdict); v != "" {
			fmt.Fprintf(&b, " (%s)", v)
		}
		b.WriteString(tail)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatWorker renders one finished worker's result (or its error).
func formatWorker(i int, r hive.Result) string {
	head := fmt.Sprintf("### ⬢ worker %d", i+1)
	if t := strings.TrimSpace(r.Task); t != "" {
		head += " — " + t
	}
	if r.Err != nil {
		out := head + "\n\n_failed: " + r.Err.Error() + "_"
		if r.Report.Status != "" {
			out += "\n\n" + r.Report.Render()
		}
		return out
	}
	body := strings.TrimSpace(r.Final)
	if body == "" {
		body = "_(no output)_"
	}
	return head + "\n\n" + body
}

// wireHivePumps binds the TUI's stream/think/progress/live pump channels onto
// a hive sub-engine so its reasoning + text deltas render live in scrollback.
// In queen mode the parent engine never runs; the planner's first call
// (decompose) and every worker / reviewer / verifier / synthesize turn live on
// sub-engines, and their ThinkCh / StreamCh / ProgressCh default to nil out of
// SpawnWorker. nil-channel sends in turn_stream.go's non-blocking selects
// swallow the delta — the user would see a spinner through the entire phase.
// Reusing the parent engine's channels (the ones the TUI's pump waits on) is
// the simplest fix: the planner, workers, reviewers, and verifier all push
// into the same render path the worker/scout roles already use.
//
// Kept as a named function so the regression test can verify all four fields
// are wired without spinning up the whole queen goroutine.
func wireHivePumps(eng *loop.Engine, live chan types.Message, stream chan string, think chan string, progress chan int) {
	if eng == nil {
		return
	}
	eng.LiveMsgCh = live
	eng.StreamCh = stream
	eng.ThinkCh = think
	eng.ProgressCh = progress
}
