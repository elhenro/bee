package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// tutorialPhase is the stage of the first-run walkthrough.
type tutorialPhase int

const (
	tutPhaseGate tutorialPhase = iota // welcome modal: start / later / never
	tutPhaseRun                       // stepping through scripted content
)

// tutorialState drives the first-run interactive walkthrough — a faked,
// LLM-free session. Zero value = inactive. The Model owns message injection;
// this struct stays pure state so it copies cleanly across bubbletea updates.
type tutorialState struct {
	active bool
	phase  tutorialPhase
	focus  int    // gate button: 0=start 1=later 2=never
	step   int    // index into tutorialSteps (run phase)
	typing bool   // typewriter in progress for the current step
	typed  string // assistant text revealed so far
	full   string // target text for the current step
}

// tutorialTickMsg drives the typewriter reveal during the run phase.
type tutorialTickMsg struct{}

// openTutorialMsg asks Model.Update to replay the walkthrough (from /tutorial).
type openTutorialMsg struct{}

// typewriter pacing: reveal tutorialTypeChars runes every tutorialTypeInterval.
const (
	tutorialTypeInterval = 18 * time.Millisecond
	tutorialTypeChars    = 3
)

func tutorialTickCmd() tea.Cmd {
	return tea.Tick(tutorialTypeInterval, func(time.Time) tea.Msg { return tutorialTickMsg{} })
}

// renderTutorial draws the overlay for the active walkthrough — the welcome
// gate or the per-step coach box. Caller overlays it on the main frame.
func (m Model) renderTutorial() string {
	if !m.tutorial.active {
		return ""
	}
	if m.tutorial.phase == tutPhaseGate {
		return m.renderTutorialGate()
	}
	return m.renderTutorialCoach()
}

func (m Model) renderTutorialGate() string {
	title := m.styles.ModalTitle.Render("welcome to bee 🐝")
	body := "a short guided tour of the basics — input, tool calls, roles,\nand slash commands. nothing real runs; it's a safe demo."
	labels := []string{"[1] start tour", "[2] maybe later", "[3] never show again"}
	btns := make([]string, len(labels))
	for i, lbl := range labels {
		if i == m.tutorial.focus {
			btns[i] = m.styles.ButtonHot.Render(lbl)
		} else {
			btns[i] = m.styles.Button.Render(lbl)
		}
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, btns[0], "  ", btns[1], "  ", btns[2])
	return m.styles.Modal.Render(strings.Join([]string{
		title, "", body, "", row, "",
		StyleLabel.Render("enter pick · ←/→ move · esc later"),
	}, "\n"))
}

func (m Model) renderTutorialCoach() string {
	steps := tutorialSteps()
	if m.tutorial.step >= len(steps) {
		return ""
	}
	st := steps[m.tutorial.step]
	title := m.styles.ModalTitle.Render(
		fmt.Sprintf("tour · step %d/%d · %s", m.tutorial.step+1, len(steps), st.title))
	var hint string
	switch {
	case m.tutorial.typing:
		hint = "enter skip typing · esc exit"
	case m.tutorial.step == len(steps)-1:
		hint = "enter finish · esc exit"
	default:
		hint = "enter next · esc exit"
	}
	return m.styles.Modal.Render(strings.Join([]string{
		title, "", st.coach, "", StyleLabel.Render(hint),
	}, "\n"))
}
