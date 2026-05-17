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
	ApprovalAllow ApprovalDecision = "allow"
	ApprovalDeny  ApprovalDecision = "deny"
)

// ApprovalRequest is what the Engine (eventually) hands to the modal.
type ApprovalRequest struct {
	ToolName string
	Action   string // human-readable: "run shell: rm -rf /tmp/x"
	UseID    string // tool_use id, echoed back in the decision
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
	// focusAllow toggles which button is highlighted (default: allow)
	focusAllow bool
	// out is the channel the parent passes for Engine wakeup.
	out chan<- ApprovalDecisionMsg
}

// NewApprovalModel returns a fresh, inactive modal.
func NewApprovalModel(styles Styles, keys KeyMap) ApprovalModel {
	return ApprovalModel{styles: styles, keys: keys, focusAllow: true}
}

// SetOutput wires the engine-facing channel. Pass nil to detach.
func (m *ApprovalModel) SetOutput(ch chan<- ApprovalDecisionMsg) {
	m.out = ch
}

// Show opens the modal for the given request.
func (m *ApprovalModel) Show(req ApprovalRequest) {
	m.Request = req
	m.Active = true
	m.focusAllow = true
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
		return m.decide(ApprovalAllow)
	case key.Matches(km, m.keys.ApproveDeny):
		return m.decide(ApprovalDeny)
	case km.String() == "tab" || km.String() == "right" || km.String() == "left":
		m.focusAllow = !m.focusAllow
		return m, nil
	}
	return m, nil
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

	var allowBtn, denyBtn string
	if m.focusAllow {
		allowBtn = m.styles.ButtonHot.Render("[a]llow")
		denyBtn = m.styles.Button.Render("[d]eny")
	} else {
		allowBtn = m.styles.Button.Render("[a]llow")
		denyBtn = m.styles.ButtonHot.Render("[d]eny")
	}

	body := strings.Join([]string{
		title,
		"",
		"tool:   " + tool,
		"action: " + action,
		"",
		lipgloss.JoinHorizontal(lipgloss.Top, allowBtn, "  ", denyBtn),
	}, "\n")
	return m.styles.Modal.Render(body)
}
