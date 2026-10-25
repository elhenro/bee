package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/types"
)

// PlanProceedMsg is published when the user picks an option from the post-scout
// picker. Role is the role to switch into ("" = keep scouting, no-op); Yolo arms
// the auto-approve toggle; Fresh clears context first and seeds the new turn
// with the plan text. A non-empty Role always auto-submits a continuation turn.
type PlanProceedMsg struct {
	Role  string
	Yolo  bool
	Fresh bool
}

// planOption is one row in the post-scout picker.
type planOption struct {
	label string
	role  string // target role; "" means keep scouting
	yolo  bool
	fresh bool
}

// PlanModeModel is the inline picker shown after a clean scout turn. It fixes
// the scout→build trap: scout strips mutators, so "yes do it" used to silently
// no-op. The picker lets the user switch into worker (and optionally start a
// fresh session carrying just the plan) in one keystroke. Inactive = renders
// nothing.
type PlanModeModel struct {
	optionList
	options []planOption
}

// NewPlanModeModel returns a fresh, inactive picker.
func NewPlanModeModel(styles Styles) PlanModeModel {
	return PlanModeModel{optionList: optionList{styles: &styles}}
}

// Show opens the picker. Focus defaults to the trailing "keep scouting" option
// so a reflexive enter never switches role or fires a build turn; the build
// actions are one number-key away.
func (m *PlanModeModel) Show() {
	opts := []planOption{
		{label: "Build it (worker)", role: "worker"},
		{label: "Build it in a fresh session — clears context, keeps the plan", role: "worker", fresh: true},
		{label: "Build it (worker + yolo — auto-approves commands)", role: "worker", yolo: true},
		{label: "Keep scouting (stay in scout)", role: ""},
	}
	m.options = opts
	m.Active = true
	m.focus = len(opts) - 1
}

// Update handles picker key events. Returns the model + an optional cmd that
// publishes the choice. Caller forwards the cmd.
func (m PlanModeModel) Update(msg tea.Msg) (PlanModeModel, tea.Cmd) {
	if idx := m.key(msg, len(m.options)); idx >= 0 {
		return m.pick(idx)
	}
	if !m.Active {
		m.options = nil
	}
	return m, nil
}

func (m PlanModeModel) pick(idx int) (PlanModeModel, tea.Cmd) {
	opt := m.options[idx]
	m.Active = false
	m.options = nil
	return m, func() tea.Msg {
		return PlanProceedMsg{Role: opt.role, Yolo: opt.yolo, Fresh: opt.fresh}
	}
}

// View renders the option list under the live region.
func (m PlanModeModel) View() string {
	if !m.Active {
		return ""
	}
	labels := make([]string, len(m.options))
	for i, opt := range m.options {
		labels[i] = opt.label
	}
	head := m.styles.WarnBadge.Render(" SCOUT READY ") + " " + m.styles.Dim.Render("how do you want to proceed?")
	return m.render(head, labels, "↑↓ move · 1-9 pick · enter select · esc keep scouting")
}

// continuePrompt is submitted when the user picks a build option without a
// fresh session — the plan is still in context. Worker may have been in scout
// last turn; spell out the role switch so a model that pattern-matches on
// "above" + recent scout context doesn't keep acting as scout (no shell/git
// tools). Tools the model is allowed this turn are the full surface, not the
// scout whitelist.
const continuePrompt = "You are now in worker mode with the full tool surface " +
	"(bash, read, write, edit, search, glob, ls, etc.). The plan is the assistant " +
	"turn above this one. Implement it now — run commands, edit files, and stop " +
	"only when the plan is fully executed. Do not re-plan and do not stay in " +
	"research mode."

// freshContinuePrompt wraps the plan text for a cleared session so the model
// still has the plan even though the planning conversation was dropped.
func freshContinuePrompt(plan string) string {
	plan = strings.TrimSpace(plan)
	if plan == "" {
		return continuePrompt
	}
	return "You are in worker mode with the full tool surface (bash, read, " +
		"write, edit, search, glob, ls, etc.).\n\n" +
		"Here is the plan to implement:\n\n" + plan +
		"\n\nExecute it now. Run commands, edit files, and stop only when the " +
		"plan is fully implemented. Do not re-plan and do not stay in research " +
		"mode."
}

// freshDisplay is the scrollback bubble for a fresh-session handoff. The full
// plan goes to the model but is hidden from scrollback; surface a marker with
// the carried length so a wedge can't be misread as a dropped plan.
func freshDisplay(plan string) string {
	plan = strings.TrimSpace(plan)
	if plan == "" {
		return continuePrompt
	}
	return continuePrompt + " (fresh session · plan carried: " +
		strconv.Itoa(len(plan)) + " chars)"
}

// lastAssistantText returns the text of the last assistant message — the plan
// the model just wrote. Empty when there's no assistant turn.
func lastAssistantText(msgs []types.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == types.RoleAssistant {
			return textOf(msgs[i])
		}
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
	m.dropFlushed()
	m.lastErr = ""
	m.state = StateIdle
	m.printedCount = 0
	return m, nil
}

// onPlanProceed applies a post-scout pick: switch role (+ optional yolo),
// optionally clear context for a fresh session, then auto-submit a continuation
// turn so scout hands off to building instead of silently no-opping.
func (m Model) onPlanProceed(c PlanProceedMsg) (tea.Model, tea.Cmd) {
	plan := m.pendingPlan
	m.pendingPlan = ""
	// keep scouting: leave role and context untouched.
	if c.Role == "" {
		return m, nil
	}
	m.role = c.Role
	m.thinking = roleThinking(c.Role)
	if c.Yolo {
		m.yolo = true
		_ = PersistSetting("", "yolo", true)
	}
	m.mirrorToEngine()
	if c.Fresh {
		nm, err := m.clearForFreshSession()
		if err != nil {
			// rollout swap failed — fall back to an in-context proceed.
			m.lastErr = err.Error()
			return m.submit(continuePrompt)
		}
		// long prompt to the model, short bubble in scrollback (with a marker
		// that the plan was carried, so a wedge isn't misread as a lost plan).
		return nm.submitWithDisplay(freshContinuePrompt(plan), freshDisplay(plan))
	}
	return m.submit(continuePrompt)
}
