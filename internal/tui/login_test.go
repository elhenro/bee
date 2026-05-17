package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/commands"
)

// loginSideStub satisfies the slice of commands.Side that LoginPane needs.
// Only LoginStatus, Login, and Logout are exercised; everything else is a
// minimal no-op so the stub stays focused.
type loginSideStub struct {
	statuses        []commands.ProviderAuth
	loginCalled     string
	loginErr        error
	logoutCalled    string
	saveKeyProvider string
	saveKeyValue    string
	saveKeyErr      error
}

func (s *loginSideStub) Compact(context.Context) error                  { return nil }
func (s *loginSideStub) SwitchModel(string) error                       { return nil }
func (s *loginSideStub) SwitchProviderModel(string, string) error       { return nil }
func (s *loginSideStub) OpenPicker() error                              { return nil }
func (s *loginSideStub) ListSessions() ([]string, error)                { return nil, nil }
func (s *loginSideStub) OpenSession(string) error                       { return nil }
func (s *loginSideStub) NewSession() error                              { return nil }
func (s *loginSideStub) CopyLast() error                                { return nil }
func (s *loginSideStub) Quit()                                          {}
func (s *loginSideStub) OpenTree() error                                { return nil }
func (s *loginSideStub) OpenCost() error                                { return nil }
func (s *loginSideStub) ForkSession(string) error                       { return nil }
func (s *loginSideStub) CloneSession() error                            { return nil }
func (s *loginSideStub) Login(_ context.Context, p string) error {
	s.loginCalled = p
	return s.loginErr
}
func (s *loginSideStub) Logout(p string) error                          { s.logoutCalled = p; return nil }
func (s *loginSideStub) SaveAPIKey(p, k string) error {
	s.saveKeyProvider, s.saveKeyValue = p, k
	return s.saveKeyErr
}
func (s *loginSideStub) LoginStatus() []commands.ProviderAuth           { return s.statuses }
func (s *loginSideStub) OpenLogin() error                               { return nil }
func (s *loginSideStub) OpenResume() error                              { return nil }
func (s *loginSideStub) SetThinking(string) error                       { return nil }
func (s *loginSideStub) GetThinking() string                            { return "off" }
func (s *loginSideStub) OpenEffortPicker() error                        { return nil }
func (s *loginSideStub) SetShowThoughts(bool) error                     { return nil }
func (s *loginSideStub) GetShowThoughts() bool                          { return false }
func (s *loginSideStub) OpenSettings() error                            { return nil }
func (s *loginSideStub) SetVerbose(bool) error                          { return nil }
func (s *loginSideStub) GetVerbose() bool                               { return false }
func (s *loginSideStub) SetCompact(bool) error                          { return nil }
func (s *loginSideStub) GetCompact() bool                               { return false }
func (s *loginSideStub) SetShowNudges(bool) error                       { return nil }
func (s *loginSideStub) GetShowNudges() bool                            { return false }
func (s *loginSideStub) OpenAgentView() error                           { return nil }
func (s *loginSideStub) SetShowContextBar(bool) error                   { return nil }
func (s *loginSideStub) GetShowContextBar() bool                        { return false }
func (s *loginSideStub) SetHighlight(bool) error                        { return nil }
func (s *loginSideStub) GetHighlight() bool                             { return true }
func (s *loginSideStub) SetShellBangSilent(bool) error                  { return nil }
func (s *loginSideStub) GetShellBangSilent() bool                       { return true }

func TestLoginPane_OpensAndRendersProviders(t *testing.T) {
	side := &loginSideStub{statuses: []commands.ProviderAuth{
		{Name: "anthropic", HasOAuth: true, TokenSaved: true},
		{Name: "openai", EnvKey: "OPENAI_API_KEY"},
	}}
	p := NewLoginPane(side)
	p, _ = p.Update(ToggleLoginPaneMsg{})
	if !p.Open() {
		t.Fatal("pane should open on toggle")
	}
	out := p.View(80, 24)
	for _, want := range []string{"Login", "anthropic", "openai", "token saved", "OPENAI_API_KEY"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q in %q", want, out)
		}
	}
}

func TestLoginPane_ArrowMovesCursor(t *testing.T) {
	side := &loginSideStub{statuses: []commands.ProviderAuth{
		{Name: "a"}, {Name: "b"}, {Name: "c"},
	}}
	p := NewLoginPane(side)
	p, _ = p.Update(ToggleLoginPaneMsg{})
	if p.cursor != 0 {
		t.Fatalf("initial cursor: %d", p.cursor)
	}
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyDown})
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if p.cursor != 2 {
		t.Fatalf("down twice: cursor=%d", p.cursor)
	}
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if p.cursor != 2 {
		t.Errorf("cursor must clamp at end, got %d", p.cursor)
	}
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyUp})
	if p.cursor != 1 {
		t.Errorf("up: cursor=%d", p.cursor)
	}
}

func TestLoginPane_EnterTriggersLoginOnOAuthProvider(t *testing.T) {
	side := &loginSideStub{statuses: []commands.ProviderAuth{
		{Name: "anthropic", HasOAuth: true},
	}}
	p := NewLoginPane(side)
	p, _ = p.Update(ToggleLoginPaneMsg{})
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on oauth provider must return a tea.Cmd")
	}
	if !p.busy {
		t.Error("pane should be busy while login runs")
	}
	// drive the cmd; it should call Login and yield loginActionDoneMsg.
	msg := cmd()
	done, ok := msg.(loginActionDoneMsg)
	if !ok {
		t.Fatalf("expected loginActionDoneMsg, got %T", msg)
	}
	if done.provider != "anthropic" || done.action != "login" {
		t.Errorf("done = %+v", done)
	}
	if side.loginCalled != "anthropic" {
		t.Errorf("Login should have been called with anthropic, got %q", side.loginCalled)
	}
}

func TestLoginPane_EnterNoOAuthOpensKeyInput(t *testing.T) {
	side := &loginSideStub{statuses: []commands.ProviderAuth{
		{Name: "openai", EnvKey: "OPENAI_API_KEY"},
	}}
	p := NewLoginPane(side)
	p, _ = p.Update(ToggleLoginPaneMsg{})
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("enter on env-only provider must not start an async cmd")
	}
	if !p.inputting {
		t.Error("enter on non-oauth provider must enter api-key input mode")
	}
	if p.inputFor != "openai" {
		t.Errorf("inputFor = %q, want openai", p.inputFor)
	}
	if !strings.Contains(p.status, "openai") {
		t.Errorf("status should mention provider, got %q", p.status)
	}
	if side.loginCalled != "" {
		t.Errorf("Login must not be invoked for env-only provider, got %q", side.loginCalled)
	}
}

func TestLoginPane_EnterLocalProviderNoAuthNeeded(t *testing.T) {
	side := &loginSideStub{statuses: []commands.ProviderAuth{
		{Name: "ollama"}, // no oauth, no env_key
	}}
	p := NewLoginPane(side)
	p, _ = p.Update(ToggleLoginPaneMsg{})
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || p.inputting {
		t.Error("local provider should not open input nor return cmd")
	}
	if !strings.Contains(p.status, "no auth needed") {
		t.Errorf("status = %q, want no-auth hint", p.status)
	}
}

func TestLoginPane_APIKeyInputSubmitCallsSaveAPIKey(t *testing.T) {
	side := &loginSideStub{statuses: []commands.ProviderAuth{
		{Name: "omlx", EnvKey: "OMLX_API_KEY", KeyOptional: true},
	}}
	p := NewLoginPane(side)
	p, _ = p.Update(ToggleLoginPaneMsg{})
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyEnter}) // open input
	if !p.inputting {
		t.Fatalf("expected input mode, got inputting=%v", p.inputting)
	}
	// type a key
	for _, r := range "sk-omlx-test" {
		p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyEnter}) // submit
	if p.inputting {
		t.Error("input mode should close after submit")
	}
	if side.saveKeyProvider != "omlx" || side.saveKeyValue != "sk-omlx-test" {
		t.Errorf("SaveAPIKey args = (%q,%q)", side.saveKeyProvider, side.saveKeyValue)
	}
	if !strings.Contains(p.status, "key saved") {
		t.Errorf("status = %q, want success hint", p.status)
	}
}

func TestLoginPane_APIKeyInputEscCancels(t *testing.T) {
	side := &loginSideStub{statuses: []commands.ProviderAuth{
		{Name: "openai", EnvKey: "OPENAI_API_KEY"},
	}}
	p := NewLoginPane(side)
	p, _ = p.Update(ToggleLoginPaneMsg{})
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !p.inputting {
		t.Fatal("input mode should be open")
	}
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if p.inputting {
		t.Error("esc should leave input mode")
	}
	if side.saveKeyProvider != "" {
		t.Errorf("SaveAPIKey must not be called on esc, got %q", side.saveKeyProvider)
	}
}

func TestLoginPane_DLogsOut(t *testing.T) {
	side := &loginSideStub{statuses: []commands.ProviderAuth{
		{Name: "anthropic", HasOAuth: true, TokenSaved: true},
	}}
	p := NewLoginPane(side)
	p, _ = p.Update(ToggleLoginPaneMsg{})
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if cmd == nil {
		t.Fatal("d on token-saved provider must return a tea.Cmd")
	}
	msg := cmd()
	if _, ok := msg.(loginActionDoneMsg); !ok {
		t.Fatalf("expected loginActionDoneMsg, got %T", msg)
	}
	if side.logoutCalled != "anthropic" {
		t.Errorf("Logout should be called for anthropic, got %q", side.logoutCalled)
	}
}

func TestLoginPane_DNoTokenIsNoOp(t *testing.T) {
	side := &loginSideStub{statuses: []commands.ProviderAuth{
		{Name: "openai", EnvKey: "OPENAI_API_KEY"},
	}}
	p := NewLoginPane(side)
	p, _ = p.Update(ToggleLoginPaneMsg{})
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if cmd != nil {
		t.Error("d on no-token provider must not start an async cmd")
	}
	if side.logoutCalled != "" {
		t.Errorf("Logout must not be called when no token saved, got %q", side.logoutCalled)
	}
}

func TestLoginPane_EscClosesPane(t *testing.T) {
	side := &loginSideStub{}
	p := NewLoginPane(side)
	p, _ = p.Update(ToggleLoginPaneMsg{})
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if p.Open() {
		t.Fatal("esc should close")
	}
}

func TestLoginPane_LoginErrorShownInStatus(t *testing.T) {
	side := &loginSideStub{
		statuses: []commands.ProviderAuth{{Name: "anthropic", HasOAuth: true}},
		loginErr: errors.New("boom"),
	}
	p := NewLoginPane(side)
	p, _ = p.Update(ToggleLoginPaneMsg{})
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := cmd()
	p, _ = p.Update(msg)
	if !strings.Contains(p.status, "failed") || !strings.Contains(p.status, "boom") {
		t.Errorf("status must surface error, got %q", p.status)
	}
}
