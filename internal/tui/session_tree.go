package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/types"
)

// SessionTree is the Ctrl+T modal — renders the parent-pointer message tree
// for one session, highlighting the active branch. Supports cursor nav with
// Up/Down, Enter to switch active leaf, F to fork, C to clone, Esc to close.
type SessionTree struct {
	root     *types.MessageNode
	current  string                // active leaf id, highlights main branch
	selected string                // cursor id (user-highlighted node)
	flat     []*types.MessageNode  // DFS-flattened nodes, cursor steps along this
	open     bool
}

// SessionSwitchMsg requests the host swap the active leaf to LeafID.
type SessionSwitchMsg struct{ LeafID string }

// SessionForkMsg requests a fork at FromID (or full if empty).
type SessionForkMsg struct{ FromID string }

// SessionCloneMsg requests a clone of the entire current session.
type SessionCloneMsg struct{}

// NewSessionTree constructs an empty tree modal.
func NewSessionTree() *SessionTree { return &SessionTree{} }

// Init satisfies tea.Model.
func (t *SessionTree) Init() tea.Cmd { return nil }

// Update reacts to ToggleSessionTreeMsg and (when open) key events.
func (t *SessionTree) Update(msg tea.Msg) (*SessionTree, tea.Cmd) {
	switch m := msg.(type) {
	case ToggleSessionTreeMsg:
		t.open = !t.open
		if t.open && t.selected == "" {
			t.selected = t.defaultSelected()
		}
		return t, nil
	case tea.KeyMsg:
		if !t.open {
			return t, nil
		}
		switch m.String() {
		case "up", "k":
			t.moveCursor(-1)
		case "down", "j":
			t.moveCursor(1)
		case "enter":
			if t.selected != "" {
				leaf := t.selected
				t.current = leaf
				t.open = false
				return t, func() tea.Msg { return SessionSwitchMsg{LeafID: leaf} }
			}
		case "f", "F":
			from := t.selected
			t.open = false
			return t, func() tea.Msg { return SessionForkMsg{FromID: from} }
		case "c", "C":
			t.open = false
			return t, func() tea.Msg { return SessionCloneMsg{} }
		case "esc":
			t.open = false
		}
	}
	return t, nil
}

// ToggleSessionTreeMsg flips visibility.
type ToggleSessionTreeMsg struct{}

// Open reports modal visibility.
func (t *SessionTree) Open() bool { return t.open }

// Selected returns the id of the cursor-highlighted node (empty if none).
func (t *SessionTree) Selected() string { return t.selected }

// LoadMessages rebuilds the tree from a flat message slice.
func (t *SessionTree) LoadMessages(msgs []types.Message, currentLeafID string) {
	t.root = session.BuildTree(msgs)
	t.current = currentLeafID
	t.flat = flattenDFS(t.root)
	t.selected = t.defaultSelected()
}

// SetRoot lets a caller pass an already-built tree.
func (t *SessionTree) SetRoot(root *types.MessageNode, currentLeafID string) {
	t.root = root
	t.current = currentLeafID
	t.flat = flattenDFS(t.root)
	t.selected = t.defaultSelected()
}

// defaultSelected picks the current leaf if present in flat, else the first node.
func (t *SessionTree) defaultSelected() string {
	if t.current != "" {
		for _, n := range t.flat {
			if n.Msg.ID == t.current {
				return t.current
			}
		}
	}
	if len(t.flat) > 0 {
		return t.flat[0].Msg.ID
	}
	return ""
}

// moveCursor steps the selected node by delta along the DFS-flat order.
func (t *SessionTree) moveCursor(delta int) {
	if len(t.flat) == 0 {
		return
	}
	idx := -1
	for i, n := range t.flat {
		if n.Msg.ID == t.selected {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.selected = t.flat[0].Msg.ID
		return
	}
	idx += delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(t.flat) {
		idx = len(t.flat) - 1
	}
	t.selected = t.flat[idx].Msg.ID
}

// flattenDFS returns nodes in depth-first pre-order — same order they render.
func flattenDFS(root *types.MessageNode) []*types.MessageNode {
	if root == nil {
		return nil
	}
	var out []*types.MessageNode
	var walk func(n *types.MessageNode)
	walk = func(n *types.MessageNode) {
		out = append(out, n)
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return out
}

// View renders the tree. If no tree loaded, prints a friendly stub.
func (t *SessionTree) View(width, height int) string {
	if !t.open {
		return ""
	}
	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAccent)).
		Bold(true).
		Render("⬢ Session tree")
	if t.root == nil {
		body := StyleLabel.Render("(no messages yet)")
		return boxModal(title+"\n\n"+body, width, height)
	}

	activePath := map[string]bool{}
	for _, n := range session.LinearPath(t.root, t.current) {
		activePath[n.Msg.ID] = true
	}

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")
	t.renderNode(&b, t.root, "", true, true, activePath, width-4)
	b.WriteString("\n")
	b.WriteString(StyleLabel.Render("↑/↓ nav · enter switch · f fork · c clone · esc close"))
	return boxModal(b.String(), width, height)
}

func (t *SessionTree) renderNode(b *strings.Builder, n *types.MessageNode, prefix string, isLast, isRoot bool, active map[string]bool, width int) {
	if n == nil {
		return
	}
	branch := "├─ "
	nextPrefix := prefix + "│  "
	if isLast {
		branch = "└─ "
		nextPrefix = prefix + "   "
	}
	if isRoot {
		branch = ""
		nextPrefix = ""
	}

	cursor := "  "
	if n.Msg.ID == t.selected {
		cursor = "▸ "
	}

	label := nodeLabel(n)
	if width > 0 && len(cursor)+len(prefix)+len(branch)+len(label) > width {
		cut := width - len(cursor) - len(prefix) - len(branch) - 1
		if cut < 4 {
			cut = 4
		}
		if cut < len(label) {
			label = label[:cut] + "…"
		}
	}
	style := StyleLabel
	if active[n.Msg.ID] {
		style = StyleActive
	}
	if n.Msg.ID == t.selected {
		// selected cursor wins visually even if not on active path
		style = StyleActive
	}
	b.WriteString(cursor)
	b.WriteString(prefix)
	b.WriteString(StyleLabel.Render(branch))
	b.WriteString(style.Render(label))
	b.WriteString("\n")

	for i, c := range n.Children {
		t.renderNode(b, c, nextPrefix, i == len(n.Children)-1, false, active, width)
	}
}

func nodeLabel(n *types.MessageNode) string {
	if n == nil {
		return ""
	}
	id := n.Msg.ID
	if len(id) > 8 {
		id = id[:8]
	}
	preview := firstText(n.Msg)
	if len(preview) > 40 {
		preview = preview[:40] + "…"
	}
	preview = strings.ReplaceAll(preview, "\n", " ")
	return fmt.Sprintf("%s [%s] %s", id, n.Msg.Role, preview)
}

func firstText(m types.Message) string {
	for _, b := range m.Content {
		if b.Type == types.BlockText && b.Text != "" {
			return b.Text
		}
	}
	return ""
}

func boxModal(body string, width, height int) string {
	if width < 20 {
		width = 20
	}
	if height < 6 {
		height = 6
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorAccent)).
		Padding(0, 1).
		Width(width - 2).
		Height(height - 2).
		Render(body)
}
