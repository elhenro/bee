package tui

import (
	"fmt"
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
	// width is the terminal width, so View can bound the modal.
	width int
	// out is the channel the parent passes for Engine wakeup.
	out chan<- ApprovalDecisionMsg
}

// SetWidth records the terminal width so View can bound the modal.
func (m *ApprovalModel) SetWidth(w int) {
	m.width = w
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

// approvalActionLines caps how many lines of the action (command) the modal
// shows; the rest collapse into a `… +N more` note so a long script can't
// blow the box up to full-screen.
const approvalActionLines = 8

// View renders the modal box. The parent overlays it on the main view.
func (m ApprovalModel) View() string {
	if !m.Active {
		return ""
	}
	dim := lipgloss.NewStyle().Foreground(fgOyster)
	// inner content width: same bound as the question modal, shrunk on narrow
	// terminals. reserve 6 cols for border (2) + horizontal padding (4).
	inner := askModalWidth
	if m.width > 0 && m.width-6 < inner {
		inner = m.width - 6
	}
	if inner < 24 {
		inner = 24
	}

	action := strings.TrimRight(m.Request.Action, "\n")
	if action == "" {
		action = "(no detail)"
	}
	reason := m.Request.Reason
	if reason == "" {
		reason = "(unspecified)"
	}

	lines := []string{
		m.styles.ModalTitle.Render("permission request") + "  " + m.styles.ToolName.Render(m.Request.ToolName),
		"",
	}
	lines = append(lines, dim.Render("reason"))
	for _, rl := range wrapHanging(reason, inner) {
		lines = append(lines, "  "+rl)
	}
	lines = append(lines, "", dim.Render("command"))
	lines = append(lines, clipActionLines(action, inner)...)
	lines = append(lines, "")

	labels := []string{"[a]llow once", "[s]ession", "[f]orever", "[d]eny"}
	btns := make([]string, 4)
	for i, lbl := range labels {
		if i == m.focus {
			btns[i] = m.styles.ButtonHot.Render(lbl)
		} else {
			btns[i] = m.styles.Button.Render(lbl)
		}
	}
	lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, btns[0], " ", btns[1], " ", btns[2], " ", btns[3]))

	return m.styles.Modal.Width(inner + 4).Render(strings.Join(lines, "\n"))
}

// clipActionLines truncates each command line to the inner width and caps the
// total at approvalActionLines, appending a `… +N more` note when over.
func clipActionLines(action string, inner int) []string {
	raw := strings.Split(action, "\n")
	var out []string
	for _, l := range raw {
		if len(l) > inner {
			l = l[:inner-1] + "…"
		}
		out = append(out, "  "+l)
		if len(out) >= approvalActionLines && len(raw) > approvalActionLines {
			dim := lipgloss.NewStyle().Foreground(fgOyster)
			out = append(out, dim.Render(fmt.Sprintf("  … +%d more", len(raw)-approvalActionLines)))
			break
		}
	}
	return out
}
