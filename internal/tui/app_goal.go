package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/goal"
	"github.com/elhenro/bee/internal/types"
)

// goalEvalTimeout caps the side-call so a stuck provider can't wedge the loop.
const goalEvalTimeout = 30 * time.Second

// goalEvalDoneMsg carries a fast-model completion verdict back into Update.
// gen is the goalGen captured when the eval was scheduled; a mismatch (goal
// cleared or replaced meanwhile) means the verdict is stale and ignored.
type goalEvalDoneMsg struct {
	gen int
	v   goal.Verdict
	err error
}

// currentCumTokens reports cumulative session tokens (input+output) from the
// shared cost tracker. 0 when no tracker is wired — TokensSpent then reflects
// the absolute baseline so caps still behave sanely.
func currentCumTokens(m Model) int {
	if m.costs == nil {
		return 0
	}
	t := m.costs.Total()
	return t.Input + t.Output
}

// handleGoal implements the /goal subcommands. Setting a goal records state and
// submits the condition as the first turn; show/clear/stats render assistant
// text and flush.
func (m Model) handleGoal(args []string) (tea.Model, tea.Cmd) {
	// no arg: brief stats or usage hint.
	if len(args) == 0 {
		if m.goal.Active {
			line := m.goal.StatLine(currentCumTokens(m))
			if m.goal.LastReason != "" {
				line += " — " + m.goal.LastReason
			}
			return m.goalText(line)
		}
		return m.goalText("no active goal. usage: /goal <condition> | /goal show | /goal clear")
	}
	first := args[0]
	// show: multi-line status.
	if first == "show" {
		if !m.goal.Active {
			return m.goalText("no active goal")
		}
		return m.goalText(m.goal.Status(currentCumTokens(m), time.Now()))
	}
	// clear and aliases.
	if len(args) == 1 && goal.IsClearWord(first) {
		if !m.goal.Active {
			return m.goalText("no active goal")
		}
		m.goal.Clear()
		m.goalGen++
		return m.goalText("goal cleared")
	}
	// set: store state, then submit the condition as the first turn.
	cond := joinArgs(args)
	m.goal.Set(cond, currentCumTokens(m), time.Now())
	m.goalGen++
	return m.submit(cond)
}

// goalText appends an assistant message and flushes, mirroring runSlash text
// output.
func (m Model) goalText(text string) (tea.Model, tea.Cmd) {
	m.messages = append(m.messages, types.Message{
		Role:    types.RoleAssistant,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: text}},
	})
	return m, m.flush()
}

// joinArgs joins args with single spaces.
func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

// goalEvalCmd returns a cmd that asks a fast model whether the condition is
// met, based on recent messages. Copies the slice for the goroutine.
func (m Model) goalEvalCmd(gen int) tea.Cmd {
	if m.eng == nil || m.eng.Provider == nil {
		return nil
	}
	prov := m.eng.Provider
	fastModel := config.FastModelOf(m.eng.Cfg)
	cond := m.goal.Condition
	copyMsgs := append([]types.Message(nil), m.messages...)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), goalEvalTimeout)
		defer cancel()
		v, err := goal.Evaluate(ctx, prov, fastModel, cond, copyMsgs)
		return goalEvalDoneMsg{gen: gen, v: v, err: err}
	}
}

// maybeStartGoalEval runs after a clean turn finish. Ticks the goal, stops on
// caps, otherwise schedules a fresh eval.
func (m Model) maybeStartGoalEval() (Model, tea.Cmd) {
	if !m.goal.Active {
		return m, nil
	}
	m.goal.Tick()
	if exceeded, reason := m.goal.CapsExceeded(currentCumTokens(m)); exceeded {
		m.goal.Clear()
		m.goalGen++
		m.messages = append(m.messages, types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: "goal: stopped (" + reason + ")"}},
		})
		return m, m.flush()
	}
	m.goalGen++
	return m, m.goalEvalCmd(m.goalGen)
}

// onGoalEvalDone consumes a completion verdict. Stale or inactive => ignore.
// Met => clear + announce. Not met => continuation submit. Error => stop.
func (m Model) onGoalEvalDone(msg goalEvalDoneMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.goalGen || !m.goal.Active {
		return m, nil
	}
	m.goal.LastReason = msg.v.Reason
	if msg.err != nil {
		// stop on error to avoid a runaway loop against a flaky side model.
		m.goal.Clear()
		m.goalGen++
		line := lipgloss.NewStyle().Foreground(fgOyster).Italic(true).
			Render("goal: eval error (" + msg.err.Error() + "), stopped")
		return m, tea.Println(line)
	}
	if msg.v.Met {
		m.goal.Clear()
		m.goalGen++
		m.messages = append(m.messages, types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: "✓ goal achieved: " + msg.v.Reason}},
		})
		return m, m.flush()
	}
	cond := m.goal.Condition
	notice := lipgloss.NewStyle().Foreground(fgOyster).Italic(true).
		Render("goal not met (" + msg.v.Reason + ") — continuing")
	flushed := tea.Println(notice)
	nm, subCmd := m.submit(goal.Continuation(cond, msg.v.Reason))
	return nm, tea.Batch(flushed, subCmd)
}

// goalStatusLine returns the compact one-line status-bar indicator, or "".
func (m Model) goalStatusLine() string {
	if !m.goal.Active {
		return ""
	}
	return m.goal.StatLine(currentCumTokens(m))
}
