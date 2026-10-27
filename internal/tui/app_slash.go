package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/commands"
	"github.com/elhenro/bee/internal/skills"
)

// runSlash parses "/name args…", looks up the command, runs it, and
// renders the result. Unknown commands surface as a transient error.
func (m Model) runSlash(text string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(strings.TrimPrefix(text, "/"))
	if len(parts) == 0 {
		return m, nil
	}
	// /edit and /term launch external commands (editor, shell, any program)
	// in a tmux window — TUI-only, intercepted before the command registry.
	switch parts[0] {
	case "edit", "vim":
		return m, m.openEditorCmd(strings.Join(parts[1:], " "))
	case "term":
		return m, m.openTermCmd(strings.Join(parts[1:], " "))
	}
	if m.cmds == nil {
		return m.setError("no command registry")
	}
	c, ok := m.cmds.Get(parts[0])
	if !ok {
		// fall through to skills registry so "/calc" runs the calc skill
		// the same way "#calc" did from the palette. prompt and recipe
		// skills fold body into a user-turn prompt; exec skills are
		// already registered as tools by the engine.
		if m.skills != nil {
			if sk, found := m.skills.Get(parts[0]); found {
				if sk.Kind != skills.KindPrompt && sk.Kind != skills.KindRecipe {
					return m.setError("/" + parts[0] + ": skill kind " + string(sk.Kind) + " not invokable as a slash command; exec skills are auto-registered as tools and prompt/recipe bodies are folded into the user turn")
				}
				userMsg := sk.Body
				if len(parts) > 1 {
					extra := strings.Join(parts[1:], " ")
					if userMsg == "" {
						userMsg = extra
					} else {
						userMsg += "\n\nInput: " + extra
					}
				}
				if strings.TrimSpace(userMsg) == "" {
					return m.setError("/" + parts[0] + ": skill has empty body")
				}
				m.palette.Bump(parts[0])
				// grant the skill's declared plan-only tools (e.g. /plan's
				// ask_user) for this turn so they survive the mode filter even
				// in edit/auto. Non-plan-only names are ignored downstream.
				if m.eng != nil && len(sk.Tools) > 0 {
					m.eng.OnceAllowTools = sk.Tools
				}
				// render the typed command in scrollback, not the expanded body.
				return m.submitWithDisplay(userMsg, "/"+strings.Join(parts, " "))
			}
		}
		return m.setError("unknown command /" + parts[0])
	}
	m.palette.Bump(parts[0])

	// /stop cancels the in-flight turn — same path as the esc keybinding.
	// special-cased here because the cancel func lives on the Model, out of
	// reach of a registry command's Run.
	if parts[0] == "stop" {
		msg := "nothing to stop"
		if m.state == StateStreaming {
			m.cancelTurn()
			msg = "stopped"
		}
		m.messages = append(m.messages, ephemeral("("+msg+")"))
		return m, m.flush()
	}

	// /goal drives a TUI-special completion loop (set/show/clear/stats);
	// special-cased before the generic Run fallback like /compact.
	if parts[0] == "goal" {
		return m.handleGoal(parts[1:])
	}

	// /watchdog flips the session-only auto-resume toggle on the Model — out of
	// reach of a registry command's Run, like /stop.
	if parts[0] == "watchdog" {
		return m.handleWatchdog(parts[1:])
	}

	// /init submits a project-scan prompt as a user turn so the model uses its
	// own file/shell tools to inspect the repo and write AGENTS.md. Extra args
	// are appended as additional instructions.
	if parts[0] == "init" {
		body := commands.InitPrompt
		if len(parts) > 1 {
			body += "\n\nAdditional instructions: " + strings.Join(parts[1:], " ")
		}
		return m.submitWithDisplay(body, "/"+strings.Join(parts, " "))
	}

	// /remote-control is informational in the TUI: starting a server bound to
	// the live engine would race the in-flight turn. Tell the user to run the
	// standalone command instead.
	if parts[0] == "remote-control" {
		info := "remote-control runs a local web relay so another device can drive bee.\n" +
			"run `bee remote-control` in a separate terminal to start it.\n" +
			"it prints a URL + QR code to open on a phone or browser.\n" +
			"the machine must be reachable on your LAN; commands execute locally here."
		m.messages = append(m.messages, ephemeral(info))
		return m, m.flush()
	}

	// /compact runs async with a loader animation so the LLM summarization
	// call doesn't freeze the UI. State stays StateIdle; m.compacting drives
	// the loader tick. See compactDoneMsg handler in Update.
	if parts[0] == "compact" {
		if m.compacting {
			return m, nil // already running
		}
		m.compacting = true
		m.loaderFrame = 0
		eng := m.eng
		ctx := m.baseCtx()
		return m, tea.Batch(
			m.loaderTick(),
			func() tea.Msg {
				if eng == nil {
					return compactDoneMsg{}
				}
				msgs, stats, err := eng.Compact(ctx)
				return compactDoneMsg{err: err, stats: stats, msgs: msgs}
			},
		)
	}
	out, err := c.Run(m.baseCtx(), parts[1:], m.side())
	if err != nil {
		return m.setError(err.Error())
	}
	// /quit asks the TUI to exit — bubble that up as tea.Quit.
	if m.quitRequested {
		return m, tea.Quit
	}
	// commands that open a pane flag the Model (via Side); turn the flag into
	// the matching open message. /handoff arms the rescue flow first and opens
	// the same picker as /model.
	for _, r := range []struct {
		flag *bool
		open tea.Msg
	}{
		{&m.treeRequested, openTreeMsg{}},
		{&m.resumeRequested, openResumeMsg{}},
		{&m.rewindRequested, openRewindMsg{}},
		{&m.costRequested, openCostMsg{}},
		{&m.usageRequested, openUsageMsg{}},
		{&m.loginRequested, openLoginMsg{}},
		{&m.tutorialRequested, openTutorialMsg{}},
		{&m.roleRequested, openRoleMsg{}},
		{&m.effortRequested, openEffortMsg{}},
		{&m.settingsRequested, openSettingsMsg{}},
		{&m.toolsRequested, openToolsMsg{}},
		{&m.handoffRequested, openProviderMsg{}},
		{&m.pickerRequested, openProviderMsg{}},
	} {
		if *r.flag {
			*r.flag = false
			return m, func() tea.Msg { return r.open }
		}
	}
	if out == "" {
		// pure side-effect: echo a brief confirmation into scrollback.
		m.messages = append(m.messages, ephemeral("(/"+parts[0]+" done)"))
		return m, m.flush()
	}
	// command produced text — render it as assistant output, not a LLM turn.
	m.messages = append(m.messages, ephemeral(out))
	return m, m.flush()
}
