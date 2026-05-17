package tui

import (
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/session"
)

// ResumeEntry is one row in the resume picker.
type ResumeEntry struct {
	ID      string
	Created time.Time
	Preview string
}

// ResumePicker is the modal that lists past sessions newest-first and lets
// the user pick one with arrow keys + Enter. Esc/q dismisses.
type ResumePicker struct {
	open     bool
	entries  []ResumeEntry
	selected int
}

// ToggleResumePickerMsg flips visibility.
type ToggleResumePickerMsg struct{}

// ResumeSelectMsg carries the chosen session id back to the host.
type ResumeSelectMsg struct{ ID string }

// ResumeDismissedMsg fires when the user closes without selecting.
type ResumeDismissedMsg struct{}

// NewResumePicker returns an inactive picker.
func NewResumePicker() *ResumePicker { return &ResumePicker{} }

// Open reports modal visibility.
func (p *ResumePicker) Open() bool { return p.open }

// Init satisfies tea.Model.
func (p *ResumePicker) Init() tea.Cmd { return nil }

// Show loads sessions from disk and opens the modal.
func (p *ResumePicker) Show() {
	p.entries = loadResumeEntries()
	p.open = true
	p.selected = 0
}

// Hide closes the modal without selecting.
func (p *ResumePicker) Hide() { p.open = false }

// SetEntries lets tests inject a deterministic list.
func (p *ResumePicker) SetEntries(e []ResumeEntry) {
	p.entries = e
	if p.selected >= len(e) {
		p.selected = 0
	}
}

// Entries exposes the current list (for tests + view).
func (p *ResumePicker) Entries() []ResumeEntry { return p.entries }

// Selected returns the cursor index.
func (p *ResumePicker) Selected() int { return p.selected }

// Update handles key events while open.
func (p *ResumePicker) Update(msg tea.Msg) (*ResumePicker, tea.Cmd) {
	switch m := msg.(type) {
	case ToggleResumePickerMsg:
		if p.open {
			p.open = false
		} else {
			p.Show()
		}
		return p, nil
	case tea.KeyMsg:
		if !p.open {
			return p, nil
		}
		switch m.String() {
		case "up", "k", "ctrl+p":
			if p.selected > 0 {
				p.selected--
			}
		case "down", "j", "ctrl+n":
			if p.selected+1 < len(p.entries) {
				p.selected++
			}
		case "home", "g":
			p.selected = 0
		case "end", "G":
			if n := len(p.entries); n > 0 {
				p.selected = n - 1
			}
		case "enter":
			if p.selected >= 0 && p.selected < len(p.entries) {
				id := p.entries[p.selected].ID
				p.open = false
				return p, func() tea.Msg { return ResumeSelectMsg{ID: id} }
			}
		case "esc", "q":
			p.open = false
			return p, func() tea.Msg { return ResumeDismissedMsg{} }
		}
	}
	return p, nil
}

// loadResumeEntries pulls sessions from disk newest-first with first-user previews.
func loadResumeEntries() []ResumeEntry {
	sess, err := session.List()
	if err != nil || len(sess) == 0 {
		return nil
	}
	out := make([]ResumeEntry, 0, len(sess))
	for _, s := range sess {
		preview, _ := session.FirstUserText(s.ID)
		out = append(out, ResumeEntry{ID: s.ID, Created: s.Created, Preview: preview})
	}
	return out
}

// View renders the picker. Title + list + footer hint.
func (p *ResumePicker) View(width, height int) string {
	if !p.open {
		return ""
	}
	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAccent)).
		Bold(true).
		Render("⬢ Resume session")
	if len(p.entries) == 0 {
		body := StyleLabel.Render("(no past sessions)")
		return boxModal(title+"\n\n"+body+"\n\n"+StyleLabel.Render("esc close"), width, height)
	}
	inner := width - 4
	if inner < 30 {
		inner = 30
	}
	rows := p.visibleRange(height - 6)
	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")
	for i := rows.start; i < rows.end; i++ {
		e := p.entries[i]
		b.WriteString(renderResumeRow(e, i == p.selected, inner))
		b.WriteString("\n")
	}
	if rows.end < len(p.entries) || rows.start > 0 {
		hidden := len(p.entries) - (rows.end - rows.start)
		b.WriteString(StyleLabel.Render("  +" + strconv.Itoa(hidden) + " more"))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(StyleLabel.Render("↑/↓ nav · enter resume · esc close"))
	return boxModal(b.String(), width, height)
}

type rowRange struct{ start, end int }

// visibleRange computes a window around the cursor that fits in maxRows.
func (p *ResumePicker) visibleRange(maxRows int) rowRange {
	n := len(p.entries)
	if maxRows < 3 {
		maxRows = 3
	}
	if n <= maxRows {
		return rowRange{0, n}
	}
	half := maxRows / 2
	start := p.selected - half
	if start < 0 {
		start = 0
	}
	end := start + maxRows
	if end > n {
		end = n
		start = end - maxRows
	}
	return rowRange{start, end}
}

func renderResumeRow(e ResumeEntry, selected bool, width int) string {
	cursor := "  "
	idStyle := StyleLabel
	tsStyle := StyleLabel
	prevStyle := StyleLabel
	if selected {
		cursor = "▸ "
		idStyle = StyleActive
		tsStyle = StyleActive
		prevStyle = StyleActive
	}
	id := e.ID
	if len(id) > 8 {
		id = id[:8]
	}
	ts := humanAge(e.Created)
	preview := strings.ReplaceAll(e.Preview, "\n", " ")
	preview = strings.TrimSpace(preview)
	if preview == "" {
		preview = "(empty)"
	}
	// budget: cursor(2) + id(8) + space + ts(<=12) + space + preview
	head := cursor + idStyle.Render(id) + "  " + tsStyle.Render(padRightCells(ts, 10)) + "  "
	headW := lipglossWidth(head)
	maxPrev := width - headW
	if maxPrev < 10 {
		maxPrev = 10
	}
	if lipglossWidth(preview) > maxPrev {
		preview = truncateVisible(preview, maxPrev-1) + "…"
	}
	return head + prevStyle.Render(preview)
}

// humanAge renders a created-at timestamp as a compact relative age.
func humanAge(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h ago"
	case d < 7*24*time.Hour:
		return strconv.Itoa(int(d/(24*time.Hour))) + "d ago"
	default:
		return t.Format("2006-01-02")
	}
}

// padRightCells pads s with spaces on the right up to n display cells.
func padRightCells(s string, n int) string {
	w := lipglossWidth(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}
