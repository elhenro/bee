package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/ask"
)

// AskModel is the ask_user picker: a vertical list of options plus an optional
// "type my own answer" row. Self-contained component embedded in the main
// Model; renders nothing when inactive. Communicates the pick back through the
// engine-facing channel, mirroring ApprovalModel.
type AskModel struct {
	styles   Styles
	Active   bool
	useID    string
	question ask.Question
	// focus indexes the highlighted row: 0..len(options)-1 = options,
	// len(options) = custom entry (when AllowCustom).
	focus  int
	typing bool // custom-text input mode
	input  textinput.Model
	out    chan<- AskAnswerMsg
	width  int // terminal width; bounds the modal so it never overflows
}

// NewAskModel returns a fresh, inactive picker.
func NewAskModel(styles Styles) AskModel {
	ti := textinput.New()
	ti.Placeholder = "type your own answer…"
	ti.CharLimit = 400
	return AskModel{styles: styles, input: ti}
}

// SetOutput wires the engine-facing channel. Pass nil to detach.
func (m *AskModel) SetOutput(ch chan<- AskAnswerMsg) {
	m.out = ch
}

// SetWidth records the terminal width so View can bound the modal.
func (m *AskModel) SetWidth(w int) {
	m.width = w
}

// Show opens the picker for a question, defaulting focus to the recommended
// option so enter accepts it immediately.
func (m *AskModel) Show(useID string, q ask.Question) {
	m.useID = useID
	m.question = q
	m.Active = true
	m.typing = false
	m.input.SetValue("")
	m.focus = 0
	for i, o := range q.Options {
		if o.Recommended {
			m.focus = i
			break
		}
	}
}

// Hide closes the picker without publishing an answer.
func (m *AskModel) Hide() {
	m.Active = false
	m.question = ask.Question{}
}

// customIdx is the focus index of the custom-entry row, or -1 if disabled.
func (m AskModel) customIdx() int {
	if m.question.AllowCustom {
		return len(m.question.Options)
	}
	return -1
}

func (m AskModel) rowCount() int {
	if m.question.AllowCustom {
		return len(m.question.Options) + 1
	}
	return len(m.question.Options)
}

// Update handles picker key events. Returns the model + an optional cmd that
// publishes the answer. Caller forwards the cmd.
func (m AskModel) Update(msg tea.Msg) (AskModel, tea.Cmd) {
	if !m.Active {
		return m, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.typing {
		switch km.String() {
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				return m, nil // don't submit empty custom answer
			}
			return m.answer(ask.Answer{Index: -1, Text: text})
		case "esc":
			m.typing = false
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	switch km.String() {
	case "up", "k":
		m.focus = (m.focus + m.rowCount() - 1) % m.rowCount()
	case "down", "j", "tab":
		m.focus = (m.focus + 1) % m.rowCount()
	case "esc":
		return m.answer(ask.Answer{Index: -1, Dismissed: true})
	case "enter":
		if m.focus == m.customIdx() {
			m.typing = true
			m.input.Focus()
			return m, textinput.Blink
		}
		return m.answer(ask.Answer{Index: m.focus, Text: m.question.Options[m.focus].Label})
	default:
		// number keys 1-9 pick an option directly
		if len(km.String()) == 1 {
			c := km.String()[0]
			if c >= '1' && c <= '9' {
				if idx := int(c - '1'); idx < len(m.question.Options) {
					return m.answer(ask.Answer{Index: idx, Text: m.question.Options[idx].Label})
				}
			}
		}
	}
	return m, nil
}

func (m AskModel) answer(ans ask.Answer) (AskModel, tea.Cmd) {
	out := m.out
	useID := m.useID
	m.Active = false
	m.typing = false
	cmd := func() tea.Msg {
		msg := AskAnswerMsg{UseID: useID, Answer: ans}
		if out != nil {
			select {
			case out <- msg:
			default:
			}
		}
		return msg
	}
	return m, cmd
}

// askModalWidth is the content width the modal wraps to. Kept compact and
// left-aligned regardless of terminal size so the box never spans the screen.
const askModalWidth = 64

// View renders the picker box. The parent overlays it on the main view.
func (m AskModel) View() string {
	if !m.Active {
		return ""
	}
	dim := lipgloss.NewStyle().Foreground(fgOyster)
	// inner content width: cap at askModalWidth but shrink on narrow terminals.
	// reserve 6 cols for the modal's border (2) + horizontal padding (4).
	inner := askModalWidth
	if m.width > 0 && m.width-6 < inner {
		inner = m.width - 6
	}
	if inner < 24 {
		inner = 24
	}
	lines := []string{}

	title := m.styles.ModalTitle.Render("question")
	if m.question.Header != "" {
		title += "  " + m.styles.ToolName.Render(m.question.Header)
	}
	lines = append(lines, title, "")
	lines = append(lines, wrapHanging(m.question.Prompt, inner)...)
	lines = append(lines, "")

	for i, o := range m.question.Options {
		label := o.Label
		if o.Recommended {
			label += dim.Render(" (recommended)")
		}
		row := pad(i+1) + label
		if i == m.focus && !m.typing {
			row = m.styles.ButtonHot.Render("›" + row)
		} else {
			row = "  " + row
		}
		lines = append(lines, row)
		if o.Description != "" {
			for _, dl := range wrapHanging(o.Description, inner-5) {
				lines = append(lines, dim.Render("     "+dl))
			}
		}
	}

	if m.question.AllowCustom {
		ci := m.customIdx()
		if m.typing {
			lines = append(lines, "  ✎ "+m.input.View())
		} else {
			row := "  ✎ type my own answer"
			if m.focus == ci {
				row = m.styles.ButtonHot.Render("›  ✎ type my own answer")
			}
			lines = append(lines, row)
		}
	}

	hint := "↑↓ move · enter pick · esc skip"
	if m.typing {
		hint = "enter submit · esc back"
	}
	lines = append(lines, "", dim.Render(hint))
	// Width counts content + horizontal padding (4) but not the border; pass
	// inner+4 so our pre-wrapped inner-wide lines fit without lipgloss re-wrap.
	return m.styles.Modal.Width(inner + 4).Render(strings.Join(lines, "\n"))
}

func pad(n int) string {
	if n < 10 {
		return string(rune('0'+n)) + ". "
	}
	return "#. "
}
