package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/types"
)

// runSlash parses "/name args…", looks up the command, runs it, and
// renders the result. Unknown commands surface as a transient error.
func (m Model) runSlash(text string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(strings.TrimPrefix(text, "/"))
	if len(parts) == 0 {
		return m, nil
	}
	if m.cmds == nil {
		m.lastErr = "no command registry"
		m.state = StateError
		return m, nil
	}
	c, ok := m.cmds.Get(parts[0])
	if !ok {
		// fall through to skills registry so "/calc" runs the calc skill
		// the same way "#calc" did from the palette. prompt-kind skills
		// fold body into a user-turn prompt; non-prompt kinds aren't yet
		// supported here (defer to headless `bee <skill>`).
		if m.skills != nil {
			if sk, found := m.skills.Get(parts[0]); found {
				if sk.Kind != "" && sk.Kind != skills.KindPrompt {
					m.lastErr = "/" + parts[0] + ": skill kind " + string(sk.Kind) + " not supported in TUI yet; run `bee " + parts[0] + "` instead"
					m.state = StateError
					return m, nil
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
					m.lastErr = "/" + parts[0] + ": skill has empty body"
					m.state = StateError
					return m, nil
				}
				m.palette.Bump(parts[0])
				return m.submit(userMsg)
			}
		}
		m.lastErr = "unknown command /" + parts[0]
		m.state = StateError
		return m, nil
	}
	m.palette.Bump(parts[0])

	// /goal drives a TUI-special completion loop (set/show/clear/stats);
	// special-cased before the generic Run fallback like /compact.
	if parts[0] == "goal" {
		return m.handleGoal(parts[1:])
	}

	// /remote-control is informational in the TUI: starting a server bound to
	// the live engine would race the in-flight turn. Tell the user to run the
	// standalone command instead.
	if parts[0] == "remote-control" {
		info := "remote-control runs a local web relay so another device can drive bee.\n" +
			"run `bee remote-control` in a separate terminal to start it.\n" +
			"it prints a URL + QR code to open on a phone or browser.\n" +
			"the machine must be reachable on your LAN; commands execute locally here."
		m.messages = append(m.messages, types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: info}},
		})
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
		ctx := m.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		return m, tea.Batch(
			loaderTickCmd(),
			func() tea.Msg {
				if eng == nil {
					return compactDoneMsg{}
				}
				msgs, stats, err := eng.Compact(ctx)
				return compactDoneMsg{err: err, stats: stats, msgs: msgs}
			},
		)
	}
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	out, err := c.Run(ctx, parts[1:], m.side())
	if err != nil {
		m.lastErr = err.Error()
		m.state = StateError
		return m, nil
	}
	// /quit asks the TUI to exit — bubble that up as tea.Quit.
	if m.quitRequested {
		return m, tea.Quit
	}
	// /tree asks to open the modal — dispatch the open message.
	if m.treeRequested {
		m.treeRequested = false
		return m, func() tea.Msg { return openTreeMsg{} }
	}
	// /resume asks to open the resume picker.
	if m.resumeRequested {
		m.resumeRequested = false
		return m, func() tea.Msg { return openResumeMsg{} }
	}
	// /cost asks to open the cost modal.
	if m.costRequested {
		m.costRequested = false
		return m, func() tea.Msg { return openCostMsg{} }
	}
	// /login (no args) asks to open the login pane.
	if m.loginRequested {
		m.loginRequested = false
		return m, func() tea.Msg { return openLoginMsg{} }
	}
	// /effort (no args) asks to open the effort picker.
	if m.effortRequested {
		m.effortRequested = false
		return m, func() tea.Msg { return openEffortMsg{} }
	}
	// /settings asks to open the settings pane.
	if m.settingsRequested {
		m.settingsRequested = false
		return m, func() tea.Msg { return openSettingsMsg{} }
	}
	// /tools asks to open the tools toggle pane.
	if m.toolsRequested {
		m.toolsRequested = false
		return m, func() tea.Msg { return openToolsMsg{} }
	}
	// /model (no args) asks to open the provider+model picker.
	if m.pickerRequested {
		m.pickerRequested = false
		return m, func() tea.Msg { return openProviderMsg{} }
	}
	if out == "" {
		// pure side-effect: echo a brief confirmation into scrollback.
		m.messages = append(m.messages, types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: "(/" + parts[0] + " done)"}},
		})
		return m, m.flush()
	}
	// command produced text — render it as assistant output, not a LLM turn.
	m.messages = append(m.messages, types.Message{
		Role:    types.RoleAssistant,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: out}},
	})
	return m, m.flush()
}
