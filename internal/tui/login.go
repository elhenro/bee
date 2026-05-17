package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/commands"
)

// LoginPane is the interactive /login modal. Lists providers vertically;
// arrow keys move; enter triggers OAuth or (for non-oauth providers with an
// env_key) switches to an inline text input for the api key; d/x removes the
// saved token AND any saved key file; r refreshes status; esc closes.
type LoginPane struct {
	side   commands.Side
	open   bool
	cursor int
	busy   bool
	status string // last action result, shown under the list
	list   []commands.ProviderAuth

	// inputting tracks the api-key entry sub-mode. When true the modal
	// renders a single-line password input instead of the provider list.
	inputting bool
	inputFor  string // provider name being keyed in
	keyInput  textinput.Model
}

// NewLoginPane returns a closed pane bound to the TUI side. side may be
// nil; the pane renders an empty state in that case.
func NewLoginPane(s commands.Side) *LoginPane { return &LoginPane{side: s} }

// Open reports visibility.
func (p *LoginPane) Open() bool { return p != nil && p.open }

// Show opens the pane and refreshes status from the side.
func (p *LoginPane) Show() {
	if p == nil {
		return
	}
	p.open = true
	p.cursor = 0
	p.status = ""
	p.inputting = false
	p.inputFor = ""
	p.refresh()
}

// SelectProvider points the cursor at the named provider AND, if the
// provider has an api-key (non-oauth) auth scheme, jumps straight into
// the key-input sub-mode. Used by the picker's auth-error escape hatch
// so the user lands directly on the relevant prompt.
func (p *LoginPane) SelectProvider(name string) {
	if p == nil {
		return
	}
	for i, item := range p.list {
		if item.Name == name {
			p.cursor = i
			if !item.HasOAuth && item.EnvKey != "" {
				p.status = "enter api key for " + name + " (esc to cancel)"
				p.openKeyInput(name)
			}
			return
		}
	}
}

// refresh re-pulls the provider list from the side.
func (p *LoginPane) refresh() {
	if p.side == nil {
		return
	}
	p.list = p.side.LoginStatus()
	if p.cursor >= len(p.list) {
		p.cursor = 0
	}
}

// ToggleLoginPaneMsg flips visibility.
type ToggleLoginPaneMsg struct{}

// loginActionDoneMsg carries the result of an async Login/Logout call.
type loginActionDoneMsg struct {
	provider string
	action   string // "login" or "logout"
	err      error
}

// Update handles toggle/key/action messages. Returns the pane (callers
// reassign) and an optional tea.Cmd for async work.
func (p *LoginPane) Update(msg tea.Msg) (*LoginPane, tea.Cmd) {
	if p == nil {
		return p, nil
	}
	switch m := msg.(type) {
	case ToggleLoginPaneMsg:
		if p.open {
			p.open = false
			p.inputting = false
		} else {
			p.Show()
		}
		return p, nil
	case loginActionDoneMsg:
		p.busy = false
		if m.err != nil {
			p.status = m.action + " " + m.provider + " failed: " + m.err.Error()
		} else {
			p.status = m.action + " " + m.provider + " ok"
		}
		p.refresh()
		return p, nil
	case tea.KeyMsg:
		if !p.open {
			return p, nil
		}
		if p.inputting {
			return p.updateInput(m)
		}
		if p.busy {
			if m.String() == "esc" {
				p.open = false
			}
			return p, nil
		}
		switch m.String() {
		case "esc", "q":
			p.open = false
		case "down", "j":
			if p.cursor < len(p.list)-1 {
				p.cursor++
			}
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "enter", " ":
			return p, p.actionLogin()
		case "d", "x", "delete":
			return p, p.actionLogout()
		case "r":
			p.refresh()
			p.status = "refreshed"
		}
	}
	return p, nil
}

// updateInput handles key events while the api-key text input is active.
// Enter submits, esc cancels back to the provider list.
func (p *LoginPane) updateInput(m tea.KeyMsg) (*LoginPane, tea.Cmd) {
	switch m.String() {
	case "esc":
		p.inputting = false
		p.inputFor = ""
		p.status = "cancelled"
		return p, nil
	case "enter":
		key := strings.TrimSpace(p.keyInput.Value())
		if key == "" {
			p.status = "empty key — esc to cancel"
			return p, nil
		}
		name := p.inputFor
		p.inputting = false
		p.inputFor = ""
		if p.side == nil {
			p.status = "no side; can't save"
			return p, nil
		}
		if err := p.side.SaveAPIKey(name, key); err != nil {
			p.status = "save " + name + " failed: " + err.Error()
			return p, nil
		}
		p.status = "✓ key saved for " + name
		p.refresh()
		return p, nil
	}
	var cmd tea.Cmd
	p.keyInput, cmd = p.keyInput.Update(m)
	return p, cmd
}

// selected returns the highlighted provider, or nil if list is empty.
func (p *LoginPane) selected() *commands.ProviderAuth {
	if p.cursor < 0 || p.cursor >= len(p.list) {
		return nil
	}
	return &p.list[p.cursor]
}

// actionLogin runs OAuth async, or opens the api-key input for non-oauth
// providers that have an env_key. Pure-local providers (no env_key, no
// oauth) get an inline "no auth needed" status.
func (p *LoginPane) actionLogin() tea.Cmd {
	sel := p.selected()
	if sel == nil || p.side == nil {
		return nil
	}
	if !sel.HasOAuth {
		switch {
		case sel.EnvKey == "":
			p.status = sel.Name + ": local provider — no auth needed"
			return nil
		case sel.EnvSet:
			p.status = sel.Name + ": env " + sel.EnvKey + " already set — overwrite below or esc"
		case sel.KeySaved:
			p.status = "overwriting saved key for " + sel.Name + " (esc to cancel)"
		case sel.KeyOptional:
			p.status = sel.Name + ": key optional — leave blank+esc to skip, or enter to save"
		default:
			p.status = "enter api key for " + sel.Name + " (esc to cancel)"
		}
		p.openKeyInput(sel.Name)
		return nil
	}
	p.busy = true
	p.status = "logging in to " + sel.Name + "… check your browser"
	name := sel.Name
	side := p.side
	return func() tea.Msg {
		err := side.Login(context.Background(), name)
		return loginActionDoneMsg{provider: name, action: "login", err: err}
	}
}

// openKeyInput switches the modal into api-key entry mode for provider.
func (p *LoginPane) openKeyInput(provider string) {
	ti := textinput.New()
	ti.Placeholder = "paste api key…"
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.CharLimit = 256
	ti.Width = 48
	ti.Focus()
	p.keyInput = ti
	p.inputting = true
	p.inputFor = provider
}

// actionLogout removes the saved token AND key file; no-op when neither
// is present.
func (p *LoginPane) actionLogout() tea.Cmd {
	sel := p.selected()
	if sel == nil || p.side == nil {
		return nil
	}
	if !sel.TokenSaved && !sel.KeySaved {
		p.status = sel.Name + ": nothing saved"
		return nil
	}
	p.busy = true
	p.status = "removing saved auth for " + sel.Name + "…"
	name := sel.Name
	side := p.side
	return func() tea.Msg {
		err := side.Logout(name)
		return loginActionDoneMsg{provider: name, action: "logout", err: err}
	}
}
