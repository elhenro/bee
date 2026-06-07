package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/types"
)

// PlanProceedMsg is published when the user picks an option from the post-plan
// picker. Mode is the mode to switch into ("" = keep planning, no-op); Fresh
// clears context first and seeds the new turn with the plan text. A non-empty
// Mode always auto-submits a continuation turn.
type PlanProceedMsg struct {
	Mode  string
	Fresh bool
}

// planOption is one row in the post-plan picker.
type planOption struct {
	label string
	mode  string // target mode; "" means keep planning
	fresh bool
}

// PlanModeModel is the inline picker shown after a clean plan-mode turn. It
// fixes the plan→build trap: plan mode strips mutators, so "yes do it" used to
// silently no-op. The picker lets the user switch into a build mode (and
// optionally start a fresh session carrying just the plan) in one keystroke.
// Inactive = renders nothing.
type PlanModeModel struct {
	styles  Styles
	Active  bool
	options []planOption
	focus   int
	width   int
}

// NewPlanModeModel returns a fresh, inactive picker.
func NewPlanModeModel(styles Styles) PlanModeModel {
	return PlanModeModel{styles: styles}
}

// SetWidth records the terminal width so View can wrap option rows.
func (m *PlanModeModel) SetWidth(w int) { m.width = w }

// Show opens the picker. local hides the auto option — the classifier wastes
// tokens on on-host models, matching cycleMode's behavior. Focus defaults to
// the trailing "keep planning" option so a reflexive enter never switches mode
// or fires a build turn; the build actions are one number-key away.
func (m *PlanModeModel) Show(local bool) {
	opts := []planOption{
		{label: "Build it (edit mode)", mode: "edit"},
		{label: "Build it in a fresh session — clears context, keeps the plan", mode: "edit", fresh: true},
		{label: "Build it (yolo — auto-approves commands)", mode: "yolo"},
	}
	if !local {
		opts = append(opts, planOption{label: "Switch to auto", mode: "auto"})
	}
	opts = append(opts, planOption{label: "Keep planning (stay in plan)", mode: ""})
	m.options = opts
	m.Active = true
	m.focus = len(opts) - 1
}

// Hide closes the picker without publishing a choice.
func (m *PlanModeModel) Hide() {
	m.Active = false
	m.options = nil
}

// Update handles picker key events. Returns the model + an optional cmd that
// publishes the choice. Caller forwards the cmd.
func (m PlanModeModel) Update(msg tea.Msg) (PlanModeModel, tea.Cmd) {
	if !m.Active {
		return m, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch km.String() {
	case "up", "k", "shift+tab":
		m.focus = (m.focus + len(m.options) - 1) % len(m.options)
	case "down", "j", "tab":
		m.focus = (m.focus + 1) % len(m.options)
	case "esc":
		m.Hide()
		return m, nil
	case "enter":
		return m.pick(m.focus)
	default:
		if len(km.String()) == 1 {
			if c := km.String()[0]; c >= '1' && c <= '9' {
				if idx := int(c - '1'); idx < len(m.options) {
					return m.pick(idx)
				}
			}
		}
	}
	return m, nil
}

func (m PlanModeModel) pick(idx int) (PlanModeModel, tea.Cmd) {
	opt := m.options[idx]
	m.Active = false
	m.options = nil
	return m, func() tea.Msg {
		return PlanProceedMsg{Mode: opt.mode, Fresh: opt.fresh}
	}
}

// View renders the option list under the live region.
func (m PlanModeModel) View() string {
	if !m.Active {
		return ""
	}
	rail := m.styles.WarnRail.Render("▎")
	dim := m.styles.Dim
	width := m.width - 6
	if width < 20 {
		width = 20
	}
	lines := []string{rail + " " + m.styles.WarnBadge.Render(" PLAN READY ") + " " + dim.Render("how do you want to proceed?")}
	for i, opt := range m.options {
		label := pad(i+1) + opt.label
		wrapped := wrapHanging(label, width)
		for j, wl := range wrapped {
			switch {
			case j > 0:
				lines = append(lines, rail+"      "+dim.Render(wl))
			case i == m.focus:
				lines = append(lines, rail+" "+m.styles.ButtonHot.Render("›"+wl))
			default:
				lines = append(lines, rail+"   "+wl)
			}
		}
	}
	lines = append(lines, rail+" "+dim.Render("↑↓ move · 1-9 pick · enter select · esc keep planning"))
	return strings.Join(lines, "\n")
}

// continuePrompt is submitted when the user picks a build option without a
// fresh session — the plan is still in context.
const continuePrompt = "Implement the plan above."

// freshContinuePrompt wraps the plan text for a cleared session so the model
// still has the plan even though the planning conversation was dropped.
func freshContinuePrompt(plan string) string {
	plan = strings.TrimSpace(plan)
	if plan == "" {
		return continuePrompt
	}
	return "Here is the plan to implement:\n\n" + plan + "\n\nImplement it."
}

// lastAssistantText returns the text of the last assistant message — the plan
// the model just wrote. Empty when there's no assistant turn.
func lastAssistantText(msgs []types.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != types.RoleAssistant {
			continue
		}
		var b strings.Builder
		for _, blk := range msgs[i].Content {
			if blk.Type == types.BlockText {
				b.WriteString(blk.Text)
			}
		}
		return strings.TrimSpace(b.String())
	}
	return ""
}

// clearForFreshSession resets conversation state and swaps the engine rollout
// so the next turn starts with empty context. Opens the new rollout BEFORE
// closing the old one — a failed open then leaves the live session intact so
// the caller can fall back to an in-context proceed instead of bricking the
// engine on a closed rollout.
func (m Model) clearForFreshSession() (Model, error) {
	if m.eng != nil {
		roll, err := session.Open(uuid.NewString())
		if err != nil {
			return m, err
		}
		if m.eng.Sessions != nil {
			_ = m.eng.Sessions.Close()
		}
		m.eng.Sessions = roll
		m.eng.InitialMessages = nil
		if m.eng.Costs != nil {
			m.eng.Costs.Reset()
		}
	}
	m.messages = nil
	m.partial = ""
	m.streamFlushed = ""
	m.streamFenceOpen = false
	m.pendingFlushedPrefix = ""
	m.lastErr = ""
	m.state = StateIdle
	m.printedCount = 0
	return m, nil
}

// onPlanProceed applies a post-plan pick: switch mode, optionally clear context
// for a fresh session, then auto-submit a continuation turn so plan mode hands
// off to building instead of silently no-opping.
func (m Model) onPlanProceed(c PlanProceedMsg) (tea.Model, tea.Cmd) {
	plan := m.pendingPlan
	m.pendingPlan = ""
	// keep planning: leave mode and context untouched.
	if c.Mode == "" {
		return m, nil
	}
	m.mode = c.Mode
	if m.eng != nil {
		m.eng.Cfg.Mode = c.Mode
	}
	if c.Fresh {
		nm, err := m.clearForFreshSession()
		if err != nil {
			// rollout swap failed — fall back to an in-context proceed.
			m.lastErr = err.Error()
			return m.submit(continuePrompt)
		}
		// long prompt to the model, short bubble in scrollback.
		return nm.submitWithDisplay(freshContinuePrompt(plan), continuePrompt)
	}
	return m.submit(continuePrompt)
}
