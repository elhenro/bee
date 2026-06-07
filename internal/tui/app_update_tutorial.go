package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/types"
)

// onTutorialKey routes a key to the active walkthrough — gate buttons or step
// navigation. It owns message injection (m.messages + flush) so tutorialState
// stays pure data.
func (m Model) onTutorialKey(km tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.tutorial.phase == tutPhaseGate {
		return m.tutorialGateKey(km)
	}
	return m.tutorialRunKey(km)
}

func (m Model) tutorialGateKey(km tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch km.String() {
	case "esc", "2":
		return m.tutorialDismiss(false), nil
	case "3":
		return m.tutorialDismiss(true), nil
	case "1":
		return m.tutorialStart()
	case "left", "h", "shift+tab":
		m.tutorial.focus = (m.tutorial.focus + 2) % 3
		return m, nil
	case "right", "l", "tab":
		m.tutorial.focus = (m.tutorial.focus + 1) % 3
		return m, nil
	case "enter", " ":
		switch m.tutorial.focus {
		case 0:
			return m.tutorialStart()
		case 1:
			return m.tutorialDismiss(false), nil
		default:
			return m.tutorialDismiss(true), nil
		}
	}
	return m, nil
}

func (m Model) tutorialRunKey(km tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch km.String() {
	case "esc":
		return m.tutorialDismiss(true), nil
	case "enter", " ":
		if m.tutorial.typing {
			return m.tutorialSettle() // skip typewriter, reveal at once
		}
		if m.tutorial.step >= len(tutorialSteps())-1 {
			return m.tutorialDismiss(true), nil
		}
		m.tutorial.step++
		return m.tutorialEnterStep()
	}
	return m, nil
}

// tutorialStart enters the run phase at step 0.
func (m Model) tutorialStart() (tea.Model, tea.Cmd) {
	m.tutorial.phase = tutPhaseRun
	m.tutorial.step = 0
	return m.tutorialEnterStep()
}

// tutorialEnterStep injects the step's optional user bubble and arms the
// assistant typewriter.
func (m Model) tutorialEnterStep() (tea.Model, tea.Cmd) {
	steps := tutorialSteps()
	if m.tutorial.step >= len(steps) {
		return m.tutorialDismiss(true), nil
	}
	st := steps[m.tutorial.step]
	var flushCmd tea.Cmd
	if st.user != "" {
		m.messages = append(m.messages, tutEphemeral(types.RoleUser,
			types.ContentBlock{Type: types.BlockText, Text: st.user}))
		flushCmd = m.flush()
	}
	m.tutorial.typing = true
	m.tutorial.typed = ""
	m.tutorial.full = st.stream
	m.loaderFrame = 0
	if flushCmd == nil {
		return m, tutorialTickCmd()
	}
	return m, tea.Batch(flushCmd, tutorialTickCmd())
}

// onTutorialTick reveals the next slice of the current step's assistant text,
// settling the step once the full text is shown.
func (m Model) onTutorialTick(_ tutorialTickMsg) (tea.Model, tea.Cmd) {
	if !m.tutorial.active || !m.tutorial.typing {
		return m, nil
	}
	m.loaderFrame++
	full := []rune(m.tutorial.full)
	shown := len([]rune(m.tutorial.typed))
	if shown >= len(full) {
		return m.tutorialSettle()
	}
	end := shown + tutorialTypeChars
	if end >= len(full) {
		return m.tutorialSettle()
	}
	m.tutorial.typed = string(full[:end])
	return m, tutorialTickCmd()
}

// tutorialSettle finalizes the current step: the typed assistant text plus any
// extra cards land in scrollback as ephemeral messages, and the coach overlay
// flips to its "next" prompt.
func (m Model) tutorialSettle() (tea.Model, tea.Cmd) {
	steps := tutorialSteps()
	if m.tutorial.step >= len(steps) {
		return m, nil
	}
	st := steps[m.tutorial.step]
	m.tutorial.typing = false
	m.tutorial.typed = ""
	m.tutorial.full = ""
	if st.stream != "" {
		m.messages = append(m.messages, tutEphemeral(types.RoleAssistant,
			types.ContentBlock{Type: types.BlockText, Text: st.stream}))
	}
	m.messages = append(m.messages, st.extra...)
	return m, m.flush()
}

// tutorialDismiss closes the walkthrough. persist=true records tutorial_done so
// it won't reappear on the next launch (used by "never", finish, and esc-exit);
// "maybe later" passes false so the gate returns next start.
func (m Model) tutorialDismiss(persist bool) Model {
	m.tutorial = tutorialState{}
	if persist {
		if m.eng != nil {
			m.eng.Cfg.TutorialDone = true
		}
		_ = PersistSetting("", "tutorial_done", true)
	}
	return m
}

// onOpenTutorial replays the walkthrough from /tutorial — the user opted in
// explicitly, so it skips the welcome gate and starts the tour directly.
func (m Model) onOpenTutorial(_ openTutorialMsg) (tea.Model, tea.Cmd) {
	m.tutorial = tutorialState{active: true, phase: tutPhaseRun}
	return m.tutorialEnterStep()
}
