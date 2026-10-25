package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/goal"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/tools"
)

// workProvider answers work turns with a line and the judge with the next
// scripted verdict line.
type workProvider struct{ verdicts []string }

func (p *workProvider) Name() string { return "stub" }
func (p *workProvider) Stream(_ context.Context, r llm.Request) (<-chan llm.Event, error) {
	text := "worked on it"
	if strings.HasPrefix(r.System, "You are a strict completion judge") {
		text, p.verdicts = p.verdicts[0], p.verdicts[1:]
	}
	ch := make(chan llm.Event, 2)
	ch <- llm.Event{Type: llm.EventTextDelta, Delta: text}
	ch <- llm.Event{Type: llm.EventDone, StopReason: "stop"}
	close(ch)
	return ch, nil
}

// evalDoneIn runs cmd (descending into batches) and returns the goalEvalDoneMsg
// the judge goroutine produced.
func evalDoneIn(cmd tea.Cmd) (goalEvalDoneMsg, bool) {
	if cmd == nil {
		return goalEvalDoneMsg{}, false
	}
	switch msg := cmd().(type) {
	case goalEvalDoneMsg:
		return msg, true
	case tea.BatchMsg:
		for _, c := range msg {
			if done, ok := evalDoneIn(c); ok {
				return done, true
			}
		}
	}
	return goalEvalDoneMsg{}, false
}

func goalLoopModel(t *testing.T, verdicts ...string) Model {
	t.Helper()
	cfg := config.Defaults()
	cfg.Memory.Enabled = false
	eng := &loop.Engine{Provider: &workProvider{verdicts: verdicts}, Tools: tools.NewRegistry(), Cfg: cfg, Cwd: t.TempDir()}
	m := NewModel(eng, t.TempDir(), "m", "", caveman.Default)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return nm.(Model)
}

// the loop end to end: a judging row fills the gap between turn and verdict,
// a miss continues behind a notice (not a fake user bubble), a hit ends on a
// notice, and none of it reaches the model as history.
func TestGoalLoop_JudgingRowAndNotices(t *testing.T) {
	m := goalLoopModel(t, "VERDICT: UNMET — file missing", "VERDICT: MET — file exists")
	m.input.SetValue("/goal write the file")
	nm, cmd := m.handleSubmit()
	m = nm.(Model)

	first := m.messages[len(m.messages)-1]
	if first.Display != "/goal write the file" {
		t.Fatalf("first turn display = %q, want the typed command", first.Display)
	}

	done, ok := turnDoneIn(cmd)
	if !ok || done.err != nil {
		t.Fatalf("work turn: ok=%v err=%v", ok, done.err)
	}
	nm, cmd = m.Update(done)
	m = nm.(Model)
	if !m.goalJudging || !strings.Contains(stripANSI(m.View()), "Judging goal") {
		t.Fatalf("no judging row after the turn: judging=%v", m.goalJudging)
	}

	verdict, ok := evalDoneIn(cmd)
	if !ok {
		t.Fatal("no eval scheduled after the turn")
	}
	nm, cmd = m.Update(verdict)
	m = nm.(Model)
	if m.goalJudging || m.state != StateStreaming {
		t.Fatalf("after unmet verdict: judging=%v state=%v", m.goalJudging, m.state)
	}
	cont := m.messages[len(m.messages)-1]
	if note, ok := userNoticeText(cont); !ok || !strings.Contains(note, "goal not met: file missing") {
		t.Fatalf("continuation not a notice: display=%q", cont.Display)
	}

	done, _ = turnDoneIn(cmd)
	nm, cmd = m.Update(done)
	m = nm.(Model)
	verdict, _ = evalDoneIn(cmd)
	nm, _ = m.Update(verdict)
	m = nm.(Model)
	last := m.messages[len(m.messages)-1]
	if m.goal.Active || !last.Ephemeral || !strings.Contains(last.Content[0].Text, goal.AchievedPrefix+"file exists") {
		t.Fatalf("goal not closed with an achieved notice: active=%v last=%+v", m.goal.Active, last)
	}
}

// a verdict that lands after the user started their own turn must not submit
// over it; that turn's clean finish judges the goal again.
func TestGoalVerdictDuringUserTurnIsIgnored(t *testing.T) {
	m := goalLoopModel(t)
	m.goal.Set("ship it", 0, m.turnStartedAt)
	m.goalGen = 4
	m.goalJudging = true
	m.state = StateStreaming
	m.turnGen = 7

	out, cmd := m.onGoalEvalDone(goalEvalDoneMsg{gen: 4, v: goal.Verdict{Reason: "not yet"}})
	got := out.(Model)
	if cmd != nil || got.turnGen != 7 || !got.goal.Active || got.goalJudging {
		t.Fatalf("verdict over a running turn: cmd=%v turnGen=%d active=%v judging=%v", cmd != nil, got.turnGen, got.goal.Active, got.goalJudging)
	}
}
