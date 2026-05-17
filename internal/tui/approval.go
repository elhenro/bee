package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ApprovalDecision is the user's verdict on a permission request.
type ApprovalDecision string

const (
	ApprovalAllow   ApprovalDecision = "allow"   // run once
	ApprovalSession ApprovalDecision = "session" // run + cache for session
	ApprovalAlways  ApprovalDecision = "always"  // run + persist to config
	ApprovalDeny    ApprovalDecision = "deny"
)

// ApprovalRequest is what the Engine (eventually) hands to the modal.
type ApprovalRequest struct {
	ToolName string
	Action   string // human-readable: "run shell: rm -rf /tmp/x"
	Reason   string // why the cmd was flagged, e.g. "recursive delete"
	Key      string // safety.DangerousPattern key, for session + persistent cache
	UseID    string // request id, echoed back in the decision
}

// ApprovalDecisionMsg is the tea.Msg published once the user chooses.
// Engine subscribes via the channel from RegisterApproval.
type ApprovalDecisionMsg struct {
	UseID    string
	Decision ApprovalDecision
}

// ApprovalModel is a self-contained component embedded in the main Model.
// When Active is false it renders nothing.
type ApprovalModel struct {
	styles  Styles
	keys    KeyMap
	Active  bool
	Request ApprovalRequest
	// focus is the highlighted button index: 0=allow 1=session 2=always 3=deny.
	focus int
	// out is the channel the parent passes for Engine wakeup.
	out chan<- ApprovalDecisionMsg
}

// NewApprovalModel returns a fresh, inactive modal.
func NewApprovalModel(styles Styles, keys KeyMap) ApprovalModel {
	return ApprovalModel{styles: styles, keys: keys}
}

// SetOutput wires the engine-facing channel. Pass nil to detach.
func (m *ApprovalModel) SetOutput(ch chan<- ApprovalDecisionMsg) {
	m.out = ch
}

// Show opens the modal for the given request.
func (m *ApprovalModel) Show(req ApprovalRequest) {
	m.Request = req
	m.Active = true
	m.focus = 0
}

// Hide closes the modal without publishing a decision.
func (m *ApprovalModel) Hide() {
	m.Active = false
	m.Request = ApprovalRequest{}
}

// Update handles modal key events. Returns the updated model + an optional cmd
// that publishes the decision message to the program. Caller forwards the cmd.
func (m ApprovalModel) Update(msg tea.Msg) (ApprovalModel, tea.Cmd) {
	if !m.Active {
		return m, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch {
	case key.Matches(km, m.keys.ApproveAllow):
		// "enter" submits the focused button; explicit a/y still picks allow-once.
		if km.String() == "enter" {
			return m.decide(m.decisionFor(m.focus))
		}
		return m.decide(ApprovalAllow)
	case key.Matches(km, m.keys.ApproveSession):
		return m.decide(ApprovalSession)
	case key.Matches(km, m.keys.ApproveAlways):
		return m.decide(ApprovalAlways)
	case key.Matches(km, m.keys.ApproveDeny):
		return m.decide(ApprovalDeny)
	case km.String() == "tab" || km.String() == "right":
		m.focus = (m.focus + 1) % 4
		return m, nil
	case km.String() == "shift+tab" || km.String() == "left":
		m.focus = (m.focus + 3) % 4
		return m, nil
	}
	return m, nil
}

func (m ApprovalModel) decisionFor(idx int) ApprovalDecision {
	switch idx {
	case 1:
		return ApprovalSession
	case 2:
		return ApprovalAlways
	case 3:
		return ApprovalDeny
	default:
		return ApprovalAllow
	}
}

func (m ApprovalModel) decide(d ApprovalDecision) (ApprovalModel, tea.Cmd) {
	out := m.out
	useID := m.Request.UseID
	m.Active = false
	cmd := func() tea.Msg {
		dec := ApprovalDecisionMsg{UseID: useID, Decision: d}
		// best-effort fanout to engine channel; non-blocking
		if out != nil {
			select {
			case out <- dec:
			default:
			}
		}
		return dec
	}
	return m, cmd
}

// View renders the modal box. The parent overlays it on the main view.
func (m ApprovalModel) View() string {
	if !m.Active {
		return ""
	}
	title := m.styles.ModalTitle.Render("permission request")
	tool := m.styles.ToolName.Render(m.Request.ToolName)
	action := m.Request.Action
	if action == "" {
		action = "(no detail)"
	}
	reason := m.Request.Reason
	if reason == "" {
		reason = "(unspecified)"
	}

	labels := []string{"[a]llow once", "[s]ession", "[f]orever", "[d]eny"}
	btns := make([]string, 4)
	for i, lbl := range labels {
		if i == m.focus {
			btns[i] = m.styles.ButtonHot.Render(lbl)
		} else {
			btns[i] = m.styles.Button.Render(lbl)
		}
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, btns[0], "  ", btns[1], "  ", btns[2], "  ", btns[3])

	body := strings.Join([]string{
		title,
		"",
		"tool:   " + tool,
		"reason: " + reason,
		"action: " + action,
		"",
		row,
	}, "\n")
	return m.styles.Modal.Render(body)
}
