package loop

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/jobs"
	"github.com/elhenro/bee/internal/safety"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// JobNotice takes the background jobs that finished since the last call and
// turns them into one synthetic user turn. text goes to the model, display is
// the one-line scrollback form. ok is false when nothing finished.
//
// Two callers: the turn loop folds it into a running turn at the top of the
// next iteration, and an idle UI starts a turn with it. The manager hands each
// finished job out once, so they never double up.
func (e *Engine) JobNotice() (text, display string, ok bool) {
	if e.Jobs == nil {
		return "", "", false
	}
	done := e.Jobs.TakeDone()
	if len(done) == 0 {
		return "", "", false
	}
	blocks := make([]string, len(done))
	for i, j := range done {
		blocks[i] = e.jobBlock(j)
	}
	return strings.Join(blocks, "\n\n"), jobDisplay(done), true
}

// jobBlock is one job's report. Output takes the same redaction and size cap as
// a bash result: it reaches the model by a side door otherwise.
func (e *Engine) jobBlock(j jobs.Info) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[job %d %s, %s] %s", j.ID, jobVerdict(j), jobDur(j.Elapsed()), oneLineCmd(j.Label, 200))
	out := strings.TrimSpace(j.Output)
	if out == "" {
		b.WriteString("\n(no output)")
		return b.String()
	}
	if e.Cfg.Sandbox.Confined() {
		out = safety.Redact(out)
	}
	out, _ = tools.TruncateWithLimit("bash", out, config.ActiveProfile(e.Cfg).ToolOutputTokens)
	b.WriteString("\n" + out)
	return b.String()
}

func jobVerdict(j jobs.Info) string {
	switch {
	case j.State == jobs.Exited && j.Exit == 0:
		return "done, exit 0"
	case j.State == jobs.Exited:
		return fmt.Sprintf("failed, exit %d", j.Exit)
	}
	return string(j.State)
}

// jobDisplay is the single scrollback line; the parens make the TUI draw it as
// a notice instead of a user bubble.
func jobDisplay(done []jobs.Info) string {
	if len(done) == 1 {
		j := done[0]
		return fmt.Sprintf("(job %d %s · %s · %s)", j.ID, jobVerdict(j), jobDur(j.Elapsed()), oneLineCmd(j.Label, 60))
	}
	parts := make([]string, len(done))
	for i, j := range done {
		parts[i] = fmt.Sprintf("#%d %s", j.ID, strings.SplitN(jobVerdict(j), ",", 2)[0])
	}
	return fmt.Sprintf("(%d background jobs finished: %s)", len(done), strings.Join(parts, ", "))
}

func jobDur(d time.Duration) string {
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(time.Second).String()
}

func oneLineCmd(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

// injectJobNotice appends finished background jobs to the transcript as a user
// turn, so the model sees them on its next request. The UI gets the message
// live: user turns are normally withheld from that channel, but this one has
// no optimistic copy.
func (e *Engine) injectJobNotice(ctx context.Context, msgs *[]types.Message) {
	text, display, ok := e.JobNotice()
	if !ok {
		return
	}
	msg := types.Message{
		ID:       newID(),
		ParentID: lastID(*msgs),
		Role:     types.RoleUser,
		Content:  []types.ContentBlock{{Type: types.BlockText, Text: text}},
		Time:     time.Now().UTC(),
		Display:  display,
	}
	// a failed session write must not swallow the result: the model still gets it
	_ = e.appendMessage(ctx, msg)
	*msgs = append(*msgs, msg)
	if e.LiveMsgCh != nil {
		select {
		case e.LiveMsgCh <- msg:
		default:
		}
	}
}
