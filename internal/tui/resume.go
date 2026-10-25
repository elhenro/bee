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

// Show loads sessions from disk and opens the modal.
func (p *ResumePicker) Show() {
	p.entries = loadResumeEntries()
	p.open = true
	p.selected = 0
}

// SetEntries lets tests inject a deterministic list.
func (p *ResumePicker) SetEntries(e []ResumeEntry) {
	p.entries = e
	if p.selected >= len(e) {
		p.selected = 0
	}
}

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
	title := StyleActive.Render("⬢ Resume session")
	if len(p.entries) == 0 {
		body := StyleLabel.Render("(no past sessions)")
		return boxModal(title+"\n\n"+body+"\n\n"+StyleLabel.Render("esc close"), width)
	}
	inner := max(width-4, 30)
	rows := windowRange(len(p.entries), p.selected, height-6)
	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")
	for i := rows.start; i < rows.end; i++ {
		e := p.entries[i]
		b.WriteString(renderResumeRow(e, i == p.selected, inner))
		b.WriteString("\n")
	}
	if more := moreRow(rows, len(p.entries)); more != "" {
		b.WriteString(more + "\n")
	}
	b.WriteString("\n")
	b.WriteString(StyleLabel.Render("↑/↓ nav · enter resume · esc close"))
	return boxModal(b.String(), width)
}

type rowRange struct{ start, end int }

func renderResumeRow(e ResumeEntry, selected bool, width int) string {
	cursor := "  "
	idStyle := StyleLabel
	tsStyle := StyleLabel
	prevStyle := StyleLabel
	if selected {
		cursor = selectMark(true)
		idStyle = StyleActive
		tsStyle = StyleActive
		prevStyle = StyleActive
	}
	id := e.ID
	if len(id) > 8 {
		id = id[:8]
	}
	// budget: cursor(2) + id(8) + space + ts(<=12) + space + preview
	head := cursor + idStyle.Render(id) + "  " + tsStyle.Render(padRightCells(humanAge(e.Created), 10)) + "  "
	return fitRow(head, rowPreview(e.Preview), prevStyle, width)
}

// rowPreview flattens a prompt to one line, "(empty)" when blank.
func rowPreview(s string) string {
	if s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " ")); s != "" {
		return s
	}
	return "(empty)"
}

// fitRow joins the head with the preview, shrinking the preview to fit width.
// No floor: on a narrow terminal the preview shrinks to nothing rather than
// pushing the row past the edge.
func fitRow(head, preview string, style lipgloss.Style, width int) string {
	if width <= 0 {
		return head + style.Render(preview)
	}
	maxPrev := max(width-lipglossWidth(head), 0)
	if lipglossWidth(preview) > maxPrev {
		preview = truncateVisible(preview, maxPrev)
	}
	return truncateVisible(head+style.Render(preview), width)
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
