package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/update"
)

// UpdateDecision is the user's verdict on an "update available" prompt.
type UpdateDecision int

const (
	UpdateLater     UpdateDecision = iota // close modal; re-check next interval
	UpdateNow                             // apply now (this session)
	UpdateAlways                          // persist update_check=auto + apply now
	UpdateNeverAsk                        // persist update_check=off + stop probing
)

// UpdatePrompt is the four-button modal surfaced when Probe finds main ahead.
type UpdatePrompt struct {
	styles Styles
	Active bool
	Info   update.Info
	// focus is the highlighted button: 0=later 1=now 2=always 3=never
	focus int
}

// NewUpdatePrompt returns an inactive prompt.
func NewUpdatePrompt(styles Styles) UpdatePrompt {
	return UpdatePrompt{styles: styles}
}

// Show opens the modal with the probed info.
func (p *UpdatePrompt) Show(info update.Info) {
	p.Info = info
	p.Active = true
	p.focus = 1 // default to "update now" — least surprise for explicit prompt
}

// Hide closes without publishing a decision.
func (p *UpdatePrompt) Hide() {
	p.Active = false
	p.Info = update.Info{}
}

// updateDecisionMsg carries the user's choice back into Model.Update.
type updateDecisionMsg struct {
	Decision UpdateDecision
	Info     update.Info
}

// Update handles modal key events. Returns the updated prompt + an optional
// cmd carrying the decision; caller forwards the cmd. Esc dismisses as "later".
func (p UpdatePrompt) Update(msg tea.Msg) (UpdatePrompt, tea.Cmd) {
	if !p.Active {
		return p, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	switch km.String() {
	case "esc":
		return p.decide(UpdateLater)
	case "tab", "right", "l":
		p.focus = (p.focus + 1) % 4
		return p, nil
	case "shift+tab", "left", "h":
		p.focus = (p.focus + 3) % 4
		return p, nil
	case "enter", " ":
		return p.decide(p.decisionFor(p.focus))
	// single-letter shortcuts mirror the labels
	case "1":
		return p.decide(UpdateLater)
	case "2", "u", "y":
		return p.decide(UpdateNow)
	case "3", "a":
		return p.decide(UpdateAlways)
	case "4", "n":
		return p.decide(UpdateNeverAsk)
	}
	return p, nil
}

func (p UpdatePrompt) decisionFor(idx int) UpdateDecision {
	switch idx {
	case 1:
		return UpdateNow
	case 2:
		return UpdateAlways
	case 3:
		return UpdateNeverAsk
	default:
		return UpdateLater
	}
}

func (p UpdatePrompt) decide(d UpdateDecision) (UpdatePrompt, tea.Cmd) {
	info := p.Info
	p.Active = false
	cmd := func() tea.Msg { return updateDecisionMsg{Decision: d, Info: info} }
	return p, cmd
}

// View renders the modal. Caller overlays it on the main view.
func (p UpdatePrompt) View() string {
	if !p.Active {
		return ""
	}
	title := p.styles.ModalTitle.Render("bee update available")
	repo := p.Info.Repo
	if repo == "" {
		repo = "elhenro/bee"
	}
	branch := p.Info.Branch
	if branch == "" {
		branch = "main"
	}

	cur := p.Info.CurrentSHA
	if len(cur) > 7 {
		cur = cur[:7]
	}
	latest := p.Info.ShortSHA
	if latest == "" {
		latest = "?"
	}

	ahead := ""
	if p.Info.Ahead > 0 {
		ahead = " — " + plural(p.Info.Ahead, "commit") + " behind"
	}
	summary := repo + "@" + branch + ": " + cur + " → " + latest + ahead

	labels := []string{"[1] later", "[2] update now", "[3] always auto", "[4] never ask"}
	btns := make([]string, 4)
	for i, lbl := range labels {
		if i == p.focus {
			btns[i] = p.styles.ButtonHot.Render(lbl)
		} else {
			btns[i] = p.styles.Button.Render(lbl)
		}
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, btns[0], "  ", btns[1], "  ", btns[2], "  ", btns[3])

	body := strings.Join([]string{
		title,
		"",
		summary,
		"",
		row,
		"",
		StyleLabel.Render("enter pick · ←/→ move · esc later"),
	}, "\n")
	return p.styles.Modal.Render(body)
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
