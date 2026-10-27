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
	styles  *Styles
	keys    KeyMap
	Active  bool
	Request ApprovalRequest
	// focus is the highlighted button index: 0=allow 1=session 2=always 3=deny.
	focus int
	// width is the terminal width, so View can bound the modal.
	width int
	// queue holds requests that arrived while one is on screen (parallel
	// workers); each is shown in arrival order once the current one resolves.
	queue []ApprovalRequest
}

func (m ApprovalModel) isActive() bool { return m.Active }

// SetWidth records the terminal width so View can bound the modal.
func (m *ApprovalModel) SetWidth(w int) {
	m.width = w
}

// NewApprovalModel returns a fresh, inactive modal.
func NewApprovalModel(styles Styles, keys KeyMap) ApprovalModel {
	return ApprovalModel{styles: &styles, keys: keys}
}

// Show opens the modal for the given request, or queues it behind the one
// already showing so the earlier request is never overwritten.
func (m *ApprovalModel) Show(req ApprovalRequest) {
	if m.Active {
		m.queue = append(m.queue, req)
		return
	}
	m.Request = req
	m.Active = true
	m.focus = 0
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
			return m.decide(approvalChoices[m.focus].decision)
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

func (m ApprovalModel) decide(d ApprovalDecision) (ApprovalModel, tea.Cmd) {
	useID := m.Request.UseID
	m.Active = false
	if len(m.queue) > 0 {
		next := m.queue[0]
		m.queue = append([]ApprovalRequest(nil), m.queue[1:]...)
		m.Show(next)
	}
	return m, func() tea.Msg { return ApprovalDecisionMsg{UseID: useID, Decision: d} }
}

// approvalActionLines caps how many rows of the action (command) the panel
// shows; the rest collapse into a `… +N more` note so a long script can't
// blow it up to full-screen.
const approvalActionLines = 8

// approvalChoices are the decisions in focus order, each with its key and label.
var approvalChoices = [4]struct {
	key, label string
	decision   ApprovalDecision
}{
	{"a", "allow once", ApprovalAllow}, {"s", "session", ApprovalSession},
	{"f", "forever", ApprovalAlways}, {"d", "deny", ApprovalDeny},
}

// title names what is being asked: a path outside the project for the
// directory gate, the tool otherwise
func (r ApprovalRequest) title() string {
	if strings.HasPrefix(r.Key, "dir:") {
		return "outside project"
	}
	return r.ToolName
}

// View renders the panel between two hairlines, like the prompt it sits above:
// honey marker + title, the command on a dim rail, choices on one quiet row.
func (m ApprovalModel) View() string {
	if !m.Active {
		return ""
	}
	dim := lipgloss.NewStyle().Foreground(fgOyster)
	body := lipgloss.NewStyle().Foreground(fgSmoke)
	ruleW := min(max(m.width-2, 10), askModalWidth)
	if m.width <= 0 {
		ruleW = askModalWidth
	}
	inner := max(ruleW-4, 6)
	rule := " " + m.styles.Rule.Render(strings.Repeat(glyphRule, ruleW))

	head := " " + m.styles.Mark.Render(glyphBee) + " " +
		lipgloss.NewStyle().Foreground(fgButter).Bold(true).Render(m.Request.title())
	if r := m.Request.Reason; r != "" {
		head += dim.Render("  " + glyphSep + "  " + r)
	}
	lines := []string{rule, clipLine(head, ruleW), ""}

	action := strings.TrimRight(m.Request.Action, "\n")
	if action == "" {
		action = "(no detail)"
	}
	rail := dim.Render("   " + glyphDetail + " ")
	for _, l := range wrapActionLines(action, inner) {
		lines = append(lines, rail+body.Render(l))
	}
	lines = append(lines, "")

	items := make([]string, len(approvalChoices))
	for i, c := range approvalChoices {
		if i == m.focus {
			items[i] = m.styles.Mark.Render(glyphSel+" "+c.label) + " " + dim.Render(c.key)
		} else {
			items[i] = "  " + body.Render(c.label) + " " + dim.Render(c.key)
		}
	}
	for _, row := range packRow(items, ruleW-2) {
		lines = append(lines, "  "+row)
	}
	lines = append(lines, "", " "+dim.Render("←→ move "+glyphSep+" enter pick"), rule)
	return strings.Join(lines, "\n")
}

// wrapActionLines wraps each command line to the width, so the tail of a long
// `a && b | sh` stays in front of the approver, and caps the total at
// approvalActionLines with a `… +N more` note. Control bytes drop and tabs
// expand, so a row can neither outgrow w nor repaint over the panel.
func wrapActionLines(action string, w int) []string {
	var rows []string
	for _, l := range strings.Split(action, "\n") {
		rows = append(rows, wrapHanging(expandTabs(sanitizeControl(l)), w)...)
	}
	if len(rows) <= approvalActionLines {
		return rows
	}
	more := fmt.Sprintf("%s +%d more", glyphMore, len(rows)-approvalActionLines)
	return append(rows[:approvalActionLines], more)
}

// items laid out left to right, starting a new row when the next one won't fit
func packRow(items []string, width int) []string {
	var rows []string
	row, w := "", 0
	for _, it := range items {
		iw := lipgloss.Width(it)
		if row != "" && w+1+iw > width {
			rows = append(rows, row)
			row, w = "", 0
		}
		if row != "" {
			row += " "
			w++
		}
		row += it
		w += iw
	}
	if row != "" {
		rows = append(rows, row)
	}
	return rows
}
