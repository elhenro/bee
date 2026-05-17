package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Workspace is the right-pane file/diff preview. Toggled by Ctrl+W in 3A.
// SetFile loads file contents; SetDiff overlays a unified-diff annotation
// (additions green, deletions red, context dim). Both are optional — calling
// SetDiff without SetFile renders the diff alone.
type Workspace struct {
	path     string
	body     string
	diffSrc  string
	diffFile *gitdiff.File
	visible  bool
}

// NewWorkspace constructs a Workspace. 3A embeds this in its top-level Model.
func NewWorkspace() *Workspace { return &Workspace{} }

// Init satisfies tea.Model.
func (w *Workspace) Init() tea.Cmd { return nil }

// Update reacts to ToggleWorkspaceMsg. Host model owns Ctrl+W binding.
func (w *Workspace) Update(msg tea.Msg) (*Workspace, tea.Cmd) {
	switch msg.(type) {
	case ToggleWorkspaceMsg:
		w.visible = !w.visible
	}
	return w, nil
}

// ToggleWorkspaceMsg flips visibility.
type ToggleWorkspaceMsg struct{}

// Visible reports current panel state.
func (w *Workspace) Visible() bool { return w.visible }

// SetVisible forces a state (for direct host control).
func (w *Workspace) SetVisible(v bool) { w.visible = v }

// SetFile reads path into the buffer. Stat errors propagate.
func (w *Workspace) SetFile(path string) error {
	if path == "" {
		w.path = ""
		w.body = ""
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	w.path = path
	w.body = string(b)
	return nil
}

// SetDiff parses a unified-diff and stores the first file's fragments. If
// the diff is empty or malformed it returns an error.
func (w *Workspace) SetDiff(unified string) error {
	w.diffSrc = unified
	if strings.TrimSpace(unified) == "" {
		w.diffFile = nil
		return nil
	}
	files, _, err := gitdiff.Parse(strings.NewReader(unified))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		w.diffFile = nil
		return fmt.Errorf("no files in diff")
	}
	w.diffFile = files[0]
	return nil
}

// ClearDiff drops the overlay.
func (w *Workspace) ClearDiff() {
	w.diffSrc = ""
	w.diffFile = nil
}

// Path returns the currently bound file path.
func (w *Workspace) Path() string { return w.path }

// View renders the pane with a top label `path · <type>`. width/height
// constrain the box; content overflow truncated by lines.
func (w *Workspace) View(width, height int) string {
	if !w.visible {
		return ""
	}
	if width < 12 {
		width = 12
	}
	if height < 4 {
		height = 4
	}

	header := w.header(width)
	bodyHeight := height - 2 // label + spacer
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	body := w.body
	if w.path == "" && w.diffFile == nil {
		body = StyleLabel.Render("no file selected — Ctrl+W to close, or ask bee to edit a file")
	} else if w.diffFile != nil {
		body = renderDiff(w.diffFile, width-2)
	} else {
		body = clampLines(body, bodyHeight)
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(ColorAmber)).
		Width(width-2).
		Height(bodyHeight).
		Padding(0, 1).
		Render(body)
	return header + "\n" + box
}

func (w *Workspace) header(width int) string {
	if w.path == "" {
		return StyleLabel.Render("workspace · idle")
	}
	kind := strings.TrimPrefix(filepath.Ext(w.path), ".")
	if kind == "" {
		kind = "text"
	}
	if w.diffFile != nil {
		kind = "diff"
	}
	label := fmt.Sprintf("%s · %s", w.path, kind)
	if len(label) > width-2 {
		label = "…" + label[len(label)-(width-3):]
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAccent)).
		Bold(true).
		Render(label)
}

func renderDiff(f *gitdiff.File, width int) string {
	var b strings.Builder
	addStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorAddFg))
	delStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorDelFg))
	ctxStyle := StyleLabel
	for _, frag := range f.TextFragments {
		b.WriteString(ctxStyle.Render(frag.Header()))
		b.WriteString("\n")
		for _, line := range frag.Lines {
			text := strings.TrimRight(line.Line, "\n")
			if len(text) > width-2 {
				text = text[:width-2]
			}
			switch line.Op {
			case gitdiff.OpAdd:
				b.WriteString(addStyle.Render("+ " + text))
			case gitdiff.OpDelete:
				b.WriteString(delStyle.Render("- " + text))
			default:
				b.WriteString(ctxStyle.Render("  " + text))
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func clampLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n")
}
