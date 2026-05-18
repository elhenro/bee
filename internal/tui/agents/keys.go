package agents

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/agents"
	"github.com/elhenro/bee/internal/commands"
)

// paletteNavKeys are claimed by the palette while it's active — everything
// else falls through to the textarea so users keep editing the filter.
var paletteNavKeys = map[string]struct{}{
	"up": {}, "down": {}, "enter": {}, "esc": {},
	"ctrl+n": {}, "ctrl+p": {},
}

// handleKey routes a key event. The input textarea is multi-line internally
// (shift+enter inserts a newline) but in practice we keep it single-line; the
// arrow-key navigation only fires when the input is empty so typing is
// preserved.
func (m model) handleKey(km tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := km.String()
	val := m.input.Value()
	empty := strings.TrimSpace(val) == ""

	// palette claims nav keys while active; filter mirrors via Update.
	if m.palette.Active {
		if _, ok := paletteNavKeys[s]; ok {
			newP, cmd := m.palette.Update(km)
			m.palette = newP
			return m, cmd
		}
	}

	// global quit shortcuts
	switch s {
	case "ctrl+c", "esc":
		m.exitReq = true
		return m, tea.Quit
	case "ctrl+p":
		nm, cmd := m.runSlash("/model")
		return nm, cmd
	}

	// `/` on empty input opens the palette. `/` itself flows into the
	// textarea so the user sees what they're typing; filter mirrors in Update.
	if s == "/" && empty && !m.palette.Active {
		m.palette.Show("")
	}

	// list navigation — only when the input is empty, so the user can type freely.
	// arrow keys only; letter shortcuts removed because they collided with
	// typing the first letter of any task. quit is esc/ctrl+c; merge runs auto.
	if empty {
		switch s {
		case "down":
			if m.sel < len(m.flat)-1 {
				m.sel++
			}
			return m, nil
		case "up":
			if m.sel > 0 {
				m.sel--
			}
			return m, nil
		case "right", "enter":
			// enter without input: open selected agent
			if s == "enter" && len(m.flat) == 0 {
				return m, nil
			}
			if r, ok := m.selected(); ok {
				m.attachID = r.Status.SessionID
				m.exitReq = true
				return m, tea.Quit
			}
			return m, nil
		case "left":
			// left from overview is a no-op (overview IS the root)
			return m, nil
		}
	}

	// enter with non-empty input — either slash command or spawn
	if s == "enter" && !empty {
		text := strings.TrimSpace(val)
		m.input.Reset()
		if strings.HasPrefix(text, "/") {
			nm, cmd := m.runSlash(text)
			return nm, cmd
		}
		m.spawnAgent(text)
		return m, refreshCmd()
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(km)
	return m, cmd
}

// runSlash handles /model, /provider, /quit, /help. /model delegates to the
// shared command implementation used by the main TUI.
func (m model) runSlash(text string) (model, tea.Cmd) {
	parts := strings.Fields(strings.TrimPrefix(text, "/"))
	if len(parts) == 0 {
		return m, nil
	}
	switch parts[0] {
	case "model":
		out, err := commands.RunModelCommand(parts[1:], agentsModelSide{m: &m})
		if err != nil {
			m.flash(err.Error())
			return m, nil
		}
		if out != "" {
			m.flash(out)
		}
		if m.pickerOpen && m.picker != nil {
			m.picker.SetSize(m.width-4, m.height-4)
			return m, m.picker.Show()
		}
	case "provider":
		if len(parts) < 2 {
			m.flash("usage: /provider <name> (current: " + or(m.pendingProvider, "default") + ")")
			return m, nil
		}
		m.pendingProvider = parts[1]
		m.prefs.DefaultProvider = parts[1]
		if err := persistString("agents_default_provider", parts[1]); err != nil {
			m.flash("set live; persist failed: " + err.Error())
		} else {
			m.flash("next spawn → " + m.pendingProvider + " (saved as default)")
		}
	case "clear":
		res := agents.ClearMerged(m.repoRoot)
		switch {
		case len(res.Removed) == 0 && len(res.Errors) == 0:
			m.flash("nothing to clear — no merged agents")
		case len(res.Errors) > 0:
			m.flash(fmt.Sprintf("cleared %d, %d error(s)", len(res.Removed), len(res.Errors)))
		default:
			m.flash(fmt.Sprintf("cleared %d merged agent(s)", len(res.Removed)))
		}
	case "settings":
		m.settingsPane.show(m.prefs)
	case "quit", "exit":
		m.exitReq = true
	case "help":
		m.flash("keys: ↑↓ nav · →/enter open · esc quit · / commands · /clear merged · enter (text) spawn")
	default:
		m.flash("unknown command /" + parts[0] + " (open full TUI with `bee` for all slash commands)")
	}
	return m, nil
}

type agentsModelSide struct {
	m *model
}

func (s agentsModelSide) SwitchModel(name string) error {
	if s.m == nil {
		return errors.New("model: no agents model")
	}
	if name == "" {
		return errors.New("model: empty name")
	}
	s.m.pendingModel = name
	s.m.prefs.DefaultModel = name
	if err := persistString("agents_default_model", name); err != nil {
		return err
	}
	s.m.flash("next spawn → " + s.m.pendingModel + " (saved as default)")
	return nil
}

func (s agentsModelSide) SwitchProviderModel(provider, modelName string) error {
	if s.m == nil {
		return errors.New("model: no agents model")
	}
	if provider == "" {
		return errors.New("model: empty provider")
	}
	s.m.pendingProvider = provider
	s.m.prefs.DefaultProvider = provider
	if modelName != "" {
		s.m.pendingModel = modelName
		s.m.prefs.DefaultModel = modelName
	}
	if err := persistString("agents_default_provider", provider); err != nil {
		return err
	}
	if modelName != "" {
		if err := persistString("agents_default_model", modelName); err != nil {
			return err
		}
	}
	s.m.flash("next spawn → " + provider + "/" + or(modelName, s.m.pendingModel) + " (saved as default)")
	return nil
}

func (s agentsModelSide) OpenPicker() error {
	if s.m == nil || s.m.picker == nil {
		return errors.New("no picker")
	}
	s.m.pickerOpen = true
	return nil
}

func (s agentsModelSide) LoginStatus() []commands.ProviderAuth {
	if s.m == nil {
		return nil
	}
	out := make([]commands.ProviderAuth, 0, len(s.m.cfg.Providers))
	for name, p := range s.m.cfg.Providers {
		out = append(out, commands.ProviderAuth{Name: name, EnvKey: p.EnvKey, IsDefault: name == s.m.pendingProvider})
	}
	return out
}

func (m *model) applyPick(provider, modelName string) {
	if err := (agentsModelSide{m: m}).SwitchProviderModel(provider, modelName); err != nil {
		m.flash("pick failed: " + err.Error())
		return
	}
	m.pickerOpen = false
}

func or(a, b string) string {
	if a == "" {
		return b
	}
	return a
}
