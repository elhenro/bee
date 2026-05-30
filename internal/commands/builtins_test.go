package commands

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeSide tracks calls for behavior assertions.
type fakeSide struct {
	compactCalled    bool
	switchedTo       string
	listSessions     []string
	listErr          error
	opened           string
	newCalled        bool
	copyCalled       bool
	quitCalled       bool
	treeOpened       bool
	forkedFrom       string
	forkCalled       bool
	cloneCalled      bool
	loginProvider    string
	loginErr         error
	loginStatus      []ProviderAuth
	openLoginCalled  bool
	openLoginErr     error
	logoutOf         string
	logoutErr        error
	costOpened       bool
	thinking         string
	thinkErr         error
	maxIter          int
	openResumeCalled bool
	openResumeErr    error
	pickerOpened     bool
	pickerErr        error
	verbose          bool
	verboseSet       bool
	showThoughts     bool
	showThoughtsSet  bool
	compact          bool
	compactSet       bool
	showNudges       bool
	showNudgesSet    bool
	saveKeyProvider  string
	saveKeyValue     string
	saveKeyErr       error

	toolsList       []ToolInfo
	toolDisabled    map[string]bool
	lastToolToggle  string
	setToolErr      error
	addedTool       struct{ name, cmd, desc string }
	addToolErr      error
	removedTool     string
	removeToolErr   error
	openToolsCalled bool
	openToolsErr    error
}

func (f *fakeSide) Compact(context.Context) error { f.compactCalled = true; return nil }
func (f *fakeSide) SwitchModel(n string) error    { f.switchedTo = n; return nil }
func (f *fakeSide) SwitchProviderModel(p, m string) error {
	f.switchedTo = m
	return nil
}
func (f *fakeSide) OpenPicker() error               { f.pickerOpened = true; return f.pickerErr }
func (f *fakeSide) ListSessions() ([]string, error) { return f.listSessions, f.listErr }
func (f *fakeSide) OpenSession(id string) error     { f.opened = id; return nil }
func (f *fakeSide) NewSession() error               { f.newCalled = true; return nil }
func (f *fakeSide) CopyLast() error                 { f.copyCalled = true; return nil }
func (f *fakeSide) Quit()                           { f.quitCalled = true }
func (f *fakeSide) OpenTree() error                 { f.treeOpened = true; return nil }
func (f *fakeSide) OpenCost() error                 { f.costOpened = true; return nil }
func (f *fakeSide) ForkSession(id string) error     { f.forkCalled = true; f.forkedFrom = id; return nil }
func (f *fakeSide) CloneSession() error             { f.cloneCalled = true; return nil }
func (f *fakeSide) Login(_ context.Context, p string) error {
	f.loginProvider = p
	return f.loginErr
}
func (f *fakeSide) Logout(p string) error { f.logoutOf = p; return f.logoutErr }
func (f *fakeSide) SaveAPIKey(p, k string) error {
	f.saveKeyProvider, f.saveKeyValue = p, k
	return f.saveKeyErr
}
func (f *fakeSide) LoginStatus() []ProviderAuth { return f.loginStatus }
func (f *fakeSide) OpenLogin() error            { f.openLoginCalled = true; return f.openLoginErr }
func (f *fakeSide) SetThinking(level string) error {
	if f.thinkErr != nil {
		return f.thinkErr
	}
	f.thinking = level
	return nil
}
func (f *fakeSide) GetThinking() string { return f.thinking }
func (f *fakeSide) SetMaxIterations(n int) error {
	f.maxIter = n
	return nil
}
func (f *fakeSide) GetMaxIterations() int { return f.maxIter }
func (f *fakeSide) OpenResume() error {
	f.openResumeCalled = true
	return f.openResumeErr
}

// fake returns an error so the /effort command falls back to its inline
// "effort: <level>" path — which is what the existing tests assert.
func (f *fakeSide) OpenEffortPicker() error { return errors.New("no picker") }
func (f *fakeSide) SetVerbose(v bool) error {
	f.verbose = v
	f.verboseSet = true
	return nil
}
func (f *fakeSide) GetVerbose() bool { return f.verbose }
func (f *fakeSide) SetShowThoughts(v bool) error {
	f.showThoughts = v
	f.showThoughtsSet = true
	return nil
}
func (f *fakeSide) GetShowThoughts() bool {
	if !f.showThoughtsSet {
		return true
	}
	return f.showThoughts
}
func (f *fakeSide) SetCompact(v bool) error {
	f.compact = v
	f.compactSet = true
	return nil
}
func (f *fakeSide) GetCompact() bool { return f.compact }
func (f *fakeSide) SetShowNudges(v bool) error {
	f.showNudges = v
	f.showNudgesSet = true
	return nil
}
func (f *fakeSide) GetShowNudges() bool           { return f.showNudges }
func (f *fakeSide) SetShowRecap(bool) error       { return nil }
func (f *fakeSide) GetShowRecap() bool            { return false }
func (f *fakeSide) SetShowContextBar(bool) error  { return nil }
func (f *fakeSide) GetShowContextBar() bool       { return false }
func (f *fakeSide) SetHighlight(bool) error       { return nil }
func (f *fakeSide) GetHighlight() bool            { return true }
func (f *fakeSide) SetShellBangSilent(bool) error { return nil }
func (f *fakeSide) GetShellBangSilent() bool      { return true }
func (f *fakeSide) SetShowBee(bool) error         { return nil }
func (f *fakeSide) GetShowBee() bool              { return true }
func (f *fakeSide) SetShowContextPct(bool) error  { return nil }
func (f *fakeSide) GetShowContextPct() bool       { return true }
func (f *fakeSide) SetShowModel(bool) error       { return nil }
func (f *fakeSide) GetShowModel() bool            { return true }
func (f *fakeSide) SetShowCwd(bool) error         { return nil }
func (f *fakeSide) GetShowCwd() bool              { return true }
func (f *fakeSide) SetShowEffort(bool) error      { return nil }
func (f *fakeSide) GetShowEffort() bool           { return true }
func (f *fakeSide) SetShowTurnTimer(bool) error   { return nil }
func (f *fakeSide) GetShowTurnTimer() bool        { return true }
func (f *fakeSide) SetShowGitBranch(bool) error   { return nil }
func (f *fakeSide) GetShowGitBranch() bool        { return false }
func (f *fakeSide) SetShowTotalTokens(bool) error { return nil }
func (f *fakeSide) GetShowTotalTokens() bool      { return false }
func (f *fakeSide) SetShowBanner(bool) error      { return nil }
func (f *fakeSide) GetShowBanner() bool           { return true }
func (f *fakeSide) SetShowLoader(bool) error      { return nil }
func (f *fakeSide) GetShowLoader() bool           { return true }

// OpenSettings returns an error so /settings (no args) falls back to its
// inline text status — matches the headless-fallback pattern used elsewhere.
func (f *fakeSide) OpenSettings() error { return errors.New("no settings pane") }

// OpenAgentView is a no-op stub for tests that don't exercise /agent.
func (f *fakeSide) OpenAgentView() error { return nil }

// ListTools/SetToolDisabled/AddUserTool/RemoveUserTool/OpenToolsPane stubs
// for /tools tests. Track invocation so individual cases can assert.
func (f *fakeSide) ListTools() []ToolInfo {
	if f.toolsList != nil {
		return f.toolsList
	}
	return []ToolInfo{
		{Name: "bash", Description: "shell", Disabled: false, UserDefined: false},
		{Name: "read", Description: "read", Disabled: f.toolDisabled["read"], UserDefined: false},
	}
}
func (f *fakeSide) SetToolDisabled(name string, dis bool) error {
	if f.toolDisabled == nil {
		f.toolDisabled = map[string]bool{}
	}
	f.toolDisabled[name] = dis
	f.lastToolToggle = name
	return f.setToolErr
}
func (f *fakeSide) AddUserTool(name, cmd, desc string) error {
	f.addedTool = struct{ name, cmd, desc string }{name, cmd, desc}
	return f.addToolErr
}
func (f *fakeSide) RemoveUserTool(name string) error {
	f.removedTool = name
	return f.removeToolErr
}
func (f *fakeSide) OpenToolsPane() error {
	f.openToolsCalled = true
	return f.openToolsErr
}

func TestRegisterBuiltins_Names(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	want := []string{"compact", "model", "resume", "new", "clear", "copy", "quit", "exit", "help", "tree", "cost", "fork", "clone", "login", "logout", "effort", "iterations", "iter", "settings", "tools", "bg", "agent", "attach", "agents", "goal", "remote-control", "stop"}
	for _, n := range want {
		if _, ok := r.Get(n); !ok {
			t.Errorf("missing builtin %q", n)
		}
	}
	if got := len(r.List()); got != len(want) {
		t.Errorf("want %d builtins, got %d", len(want), got)
	}
}

func TestBuiltin_Compact(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("compact")
	side := &fakeSide{}
	out, err := c.Run(context.Background(), nil, side)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("expected side-effect only, got %q", out)
	}
	if !side.compactCalled {
		t.Error("Compact not called")
	}
}

func TestBuiltin_Model_OpensPicker(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("model")
	side := &fakeSide{}
	out, err := c.Run(context.Background(), nil, side)
	if err != nil {
		t.Fatal(err)
	}
	if !side.pickerOpened {
		t.Error("expected OpenPicker to be called for /model with no args")
	}
	if out != "" {
		t.Errorf("expected empty output when picker opens, got %q", out)
	}
	if side.switchedTo != "" {
		t.Errorf("should not have switched, but did: %q", side.switchedTo)
	}
}

func TestBuiltin_Model_FallbackUsage(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("model")
	side := &fakeSide{pickerErr: errors.New("no tui")}
	out, err := c.Run(context.Background(), nil, side)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "usage:") {
		t.Errorf("expected usage hint when picker unavailable, got %q", out)
	}
}

func TestBuiltin_Model_Switch(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("model")
	side := &fakeSide{}
	_, err := c.Run(context.Background(), []string{"gpt-5"}, side)
	if err != nil {
		t.Fatal(err)
	}
	if side.switchedTo != "gpt-5" {
		t.Errorf("expected switch to gpt-5, got %q", side.switchedTo)
	}
}

func TestBuiltin_Iterations_SetAndUnlimited(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("iterations")
	side := &fakeSide{}
	out, err := c.Run(context.Background(), []string{"120"}, side)
	if err != nil {
		t.Fatal(err)
	}
	if side.maxIter != 120 || !strings.Contains(out, "120") {
		t.Errorf("set 120: maxIter=%d out=%q", side.maxIter, out)
	}
	// 0 (and negatives, clamped) read back as unlimited.
	out, _ = c.Run(context.Background(), []string{"-5"}, side)
	if side.maxIter != 0 || !strings.Contains(out, "unlimited") {
		t.Errorf("unlimited: maxIter=%d out=%q", side.maxIter, out)
	}
	// non-numeric input is rejected without mutating the cap.
	out, _ = c.Run(context.Background(), []string{"lots"}, side)
	if !strings.Contains(out, "want a number") {
		t.Errorf("bad input: out=%q", out)
	}
}

func TestBuiltin_Resume_Empty(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("resume")
	out, err := c.Run(context.Background(), nil, &fakeSide{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no past") {
		t.Errorf("expected empty-list hint, got %q", out)
	}
}

func TestBuiltin_Resume_OpensPane(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("resume")
	side := &fakeSide{listSessions: []string{"abc", "def"}}
	out, err := c.Run(context.Background(), nil, side)
	if err != nil {
		t.Fatal(err)
	}
	if !side.openResumeCalled {
		t.Error("expected OpenResume to be called")
	}
	if out != "" {
		t.Errorf("expected empty output when pane opens, got %q", out)
	}
}

func TestBuiltin_Resume_FallbackList(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("resume")
	side := &fakeSide{
		listSessions:  []string{"abc", "def"},
		openResumeErr: errors.New("no tui"),
	}
	out, err := c.Run(context.Background(), nil, side)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "abc") || !strings.Contains(out, "def") {
		t.Errorf("ids missing from fallback output: %q", out)
	}
}

func TestBuiltin_Settings_NoArgsRendersStatus(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("settings")
	side := &fakeSide{} // OpenSettings errors → fallback text path
	out, err := c.Run(context.Background(), nil, side)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "verbose") || !strings.Contains(out, "show_thoughts") {
		t.Errorf("expected status output, got %q", out)
	}
}

func TestBuiltin_Settings_ToggleVerbose(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("settings")
	side := &fakeSide{}
	out, err := c.Run(context.Background(), []string{"verbose", "on"}, side)
	if err != nil {
		t.Fatal(err)
	}
	if !side.verboseSet || !side.verbose {
		t.Errorf("verbose not set on: %+v", side)
	}
	if !strings.Contains(out, "on") {
		t.Errorf("expected 'on' in output, got %q", out)
	}
}

func TestBuiltin_Settings_ToggleThoughtsOff(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("settings")
	side := &fakeSide{}
	if _, err := c.Run(context.Background(), []string{"thoughts", "off"}, side); err != nil {
		t.Fatal(err)
	}
	if !side.showThoughtsSet || side.showThoughts {
		t.Errorf("show_thoughts not set off: %+v", side)
	}
}

func TestBuiltin_Settings_FlipNoValue(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("settings")
	side := &fakeSide{} // GetVerbose=false → flip to true
	if _, err := c.Run(context.Background(), []string{"verbose"}, side); err != nil {
		t.Fatal(err)
	}
	if !side.verbose {
		t.Error("expected flip-to-true")
	}
}

func TestBuiltin_Settings_UnknownKey(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("settings")
	side := &fakeSide{}
	out, _ := c.Run(context.Background(), []string{"bogus", "on"}, side)
	if !strings.Contains(out, "unknown setting") {
		t.Errorf("expected unknown-setting message, got %q", out)
	}
}

func TestBuiltin_Resume_Error(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("resume")
	side := &fakeSide{listErr: errors.New("boom")}
	_, err := c.Run(context.Background(), nil, side)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuiltin_New(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("new")
	side := &fakeSide{}
	if _, err := c.Run(context.Background(), nil, side); err != nil {
		t.Fatal(err)
	}
	if !side.newCalled {
		t.Error("NewSession not called")
	}
}

func TestBuiltin_Clear_AliasOfNew(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("clear")
	side := &fakeSide{}
	if _, err := c.Run(context.Background(), nil, side); err != nil {
		t.Fatal(err)
	}
	if !side.newCalled {
		t.Error("NewSession not called by /clear")
	}
}

func TestBuiltin_Copy(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("copy")
	side := &fakeSide{}
	if _, err := c.Run(context.Background(), nil, side); err != nil {
		t.Fatal(err)
	}
	if !side.copyCalled {
		t.Error("CopyLast not called")
	}
}

func TestBuiltin_Quit(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("quit")
	side := &fakeSide{}
	if _, err := c.Run(context.Background(), nil, side); err != nil {
		t.Fatal(err)
	}
	if !side.quitCalled {
		t.Error("Quit not called")
	}
}

func TestBuiltin_Exit(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("exit")
	side := &fakeSide{}
	if _, err := c.Run(context.Background(), nil, side); err != nil {
		t.Fatal(err)
	}
	if !side.quitCalled {
		t.Error("Quit not called via /exit")
	}
}

func TestBuiltin_Help(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("help")
	out, err := c.Run(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"compact", "model", "help"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q in %q", want, out)
		}
	}
}

func TestBuiltin_Tree(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("tree")
	side := &fakeSide{}
	if _, err := c.Run(context.Background(), nil, side); err != nil {
		t.Fatal(err)
	}
	if !side.treeOpened {
		t.Error("OpenTree not called")
	}
}

func TestBuiltin_Fork_NoArg(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("fork")
	side := &fakeSide{}
	if _, err := c.Run(context.Background(), nil, side); err != nil {
		t.Fatal(err)
	}
	if !side.forkCalled {
		t.Error("ForkSession not called")
	}
	if side.forkedFrom != "" {
		t.Errorf("expected empty from id, got %q", side.forkedFrom)
	}
}

func TestBuiltin_Fork_WithMsgID(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("fork")
	side := &fakeSide{}
	if _, err := c.Run(context.Background(), []string{"abc123"}, side); err != nil {
		t.Fatal(err)
	}
	if side.forkedFrom != "abc123" {
		t.Errorf("forkedFrom = %q want abc123", side.forkedFrom)
	}
}

func TestBuiltin_Clone(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("clone")
	side := &fakeSide{}
	if _, err := c.Run(context.Background(), nil, side); err != nil {
		t.Fatal(err)
	}
	if !side.cloneCalled {
		t.Error("CloneSession not called")
	}
}

func TestBuiltin_Login_OpensPane(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("login")
	side := &fakeSide{}
	out, err := c.Run(context.Background(), nil, side)
	if err != nil {
		t.Fatal(err)
	}
	if !side.openLoginCalled {
		t.Error("OpenLogin must be invoked on /login (no args)")
	}
	if out != "" {
		t.Errorf("expected empty output when pane opens, got %q", out)
	}
}

func TestBuiltin_Login_FallbackWhenPaneUnavailable(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("login")
	side := &fakeSide{
		openLoginErr: errors.New("no tui"),
		loginStatus:  []ProviderAuth{{Name: "anthropic", HasOAuth: true}},
	}
	out, err := c.Run(context.Background(), nil, side)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "anthropic") || !strings.Contains(out, "usage:") {
		t.Errorf("expected fallback table when pane unavailable, got %q", out)
	}
}

func TestBuiltin_Login_CallsSide(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("login")
	side := &fakeSide{loginStatus: []ProviderAuth{
		{Name: "anthropic", HasOAuth: true, EnvKey: "ANTHROPIC_API_KEY"},
	}}
	if _, err := c.Run(context.Background(), []string{"anthropic"}, side); err != nil {
		t.Fatal(err)
	}
	if side.loginProvider != "anthropic" {
		t.Errorf("loginProvider = %q", side.loginProvider)
	}
}

func TestBuiltin_Login_DoesNotInvokeLoginWhenOpeningPane(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("login")
	side := &fakeSide{loginStatus: []ProviderAuth{
		{Name: "anthropic", HasOAuth: true, EnvKey: "ANTHROPIC_API_KEY", EnvSet: true, IsDefault: true},
	}}
	if _, err := c.Run(context.Background(), nil, side); err != nil {
		t.Fatal(err)
	}
	if side.loginProvider != "" {
		t.Errorf("Login should not be invoked on no-arg path, was: %q", side.loginProvider)
	}
}

func TestBuiltin_Login_UnknownProvider(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("login")
	side := &fakeSide{loginStatus: []ProviderAuth{
		{Name: "anthropic", HasOAuth: true},
	}}
	out, err := c.Run(context.Background(), []string{"nope"}, side)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "unknown provider") {
		t.Errorf("expected unknown-provider hint, got %q", out)
	}
	if !strings.Contains(out, "anthropic") {
		t.Errorf("expected provider list in unknown-provider response, got %q", out)
	}
	if side.loginProvider != "" {
		t.Errorf("Login must not run for unknown provider, ran: %q", side.loginProvider)
	}
}

func TestBuiltin_Login_NoOAuthOpensTUIPane(t *testing.T) {
	// non-oauth providers now route through the TUI login pane so the user
	// can enter an api key inline. The command should call OpenLogin and
	// stay silent (TUI handles rendering) when OpenLogin succeeds.
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("login")
	side := &fakeSide{loginStatus: []ProviderAuth{
		{Name: "openrouter", EnvKey: "OPENROUTER_API_KEY"},
	}}
	out, err := c.Run(context.Background(), []string{"openrouter"}, side)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("expected empty output (TUI pane handles it), got %q", out)
	}
	if !side.openLoginCalled {
		t.Error("OpenLogin should be invoked for non-oauth providers")
	}
	if side.loginProvider != "" {
		t.Errorf("Login (oauth) must not run, ran: %q", side.loginProvider)
	}
}

func TestBuiltin_Login_NoOAuthHeadlessFallback(t *testing.T) {
	// when OpenLogin errors (headless context), fall back to text hint.
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("login")
	side := &fakeSide{
		loginStatus: []ProviderAuth{
			{Name: "openrouter", EnvKey: "OPENROUTER_API_KEY"},
		},
		openLoginErr: errors.New("headless"),
	}
	out, err := c.Run(context.Background(), []string{"openrouter"}, side)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "OPENROUTER_API_KEY") {
		t.Errorf("expected env-key hint in headless fallback, got %q", out)
	}
}

func TestBuiltin_Logout_Usage(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("logout")
	out, err := c.Run(context.Background(), nil, &fakeSide{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "usage:") {
		t.Errorf("expected usage hint, got %q", out)
	}
}

func TestBuiltin_Logout_CallsSide(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("logout")
	side := &fakeSide{}
	if _, err := c.Run(context.Background(), []string{"anthropic"}, side); err != nil {
		t.Fatal(err)
	}
	if side.logoutOf != "anthropic" {
		t.Errorf("logoutOf = %q", side.logoutOf)
	}
}

func TestBuiltin_Effort_NoArg(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("effort")
	side := &fakeSide{thinking: "high"}
	out, err := c.Run(context.Background(), nil, side)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "high") {
		t.Errorf("expected current level in output, got %q", out)
	}
}

func TestBuiltin_Effort_Set(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("effort")
	side := &fakeSide{}
	out, err := c.Run(context.Background(), []string{"medium"}, side)
	if err != nil {
		t.Fatal(err)
	}
	if side.thinking != "medium" {
		t.Errorf("thinking = %q want medium", side.thinking)
	}
	if !strings.Contains(out, "medium") {
		t.Errorf("output missing level: %q", out)
	}
}

func TestBuiltin_Effort_SideError(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("effort")
	side := &fakeSide{thinkErr: errors.New("nope")}
	if _, err := c.Run(context.Background(), []string{"medium"}, side); err == nil {
		t.Fatal("expected error from Side")
	}
}

func TestBuiltin_Tools_OpensPane(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("tools")
	side := &fakeSide{}
	out, err := c.Run(context.Background(), nil, side)
	if err != nil {
		t.Fatal(err)
	}
	if !side.openToolsCalled {
		t.Error("OpenToolsPane not called for /tools (no args)")
	}
	if out != "" {
		t.Errorf("expected empty output when pane opens, got %q", out)
	}
}

func TestBuiltin_Tools_FallbackList(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("tools")
	side := &fakeSide{openToolsErr: errors.New("headless")}
	out, err := c.Run(context.Background(), nil, side)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "bash") || !strings.Contains(out, "read") {
		t.Errorf("expected tool list, got %q", out)
	}
}

func TestBuiltin_Tools_Disable(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("tools")
	side := &fakeSide{}
	out, err := c.Run(context.Background(), []string{"disable", "bash"}, side)
	if err != nil {
		t.Fatal(err)
	}
	if side.lastToolToggle != "bash" || !side.toolDisabled["bash"] {
		t.Errorf("disable did not flip bash: %+v", side.toolDisabled)
	}
	if !strings.Contains(out, "disabled") {
		t.Errorf("expected confirmation, got %q", out)
	}
}

func TestBuiltin_Tools_Enable(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("tools")
	side := &fakeSide{toolDisabled: map[string]bool{"read": true}}
	if _, err := c.Run(context.Background(), []string{"enable", "read"}, side); err != nil {
		t.Fatal(err)
	}
	if side.toolDisabled["read"] {
		t.Error("enable did not flip read")
	}
}

func TestBuiltin_Tools_AddAndRemove(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("tools")
	side := &fakeSide{}
	if _, err := c.Run(context.Background(), []string{"add", "lint", "npm", "run", "lint", "--", "run", "linter"}, side); err != nil {
		t.Fatal(err)
	}
	if side.addedTool.name != "lint" || side.addedTool.cmd != "npm run lint" || side.addedTool.desc != "run linter" {
		t.Errorf("add did not parse correctly: %+v", side.addedTool)
	}
	if _, err := c.Run(context.Background(), []string{"rm", "lint"}, side); err != nil {
		t.Fatal(err)
	}
	if side.removedTool != "lint" {
		t.Errorf("rm did not propagate name")
	}
}

func TestBuiltin_Tools_BareToggle(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, _ := r.Get("tools")
	side := &fakeSide{}
	if _, err := c.Run(context.Background(), []string{"bash"}, side); err != nil {
		t.Fatal(err)
	}
	if !side.toolDisabled["bash"] {
		t.Error("bare /tools NAME should disable an enabled tool")
	}
	if _, err := c.Run(context.Background(), []string{"bash"}, side); err != nil {
		t.Fatal(err)
	}
	// second call sees side.ListTools() still reporting bash enabled (stub),
	// so it flips to disabled again — that's fine; what matters is reaching
	// SetToolDisabled with a non-error path.
	if side.lastToolToggle != "bash" {
		t.Errorf("toggle name lost: %q", side.lastToolToggle)
	}
}

func TestBuiltins_NilSideSafe(t *testing.T) {
	// All built-ins must tolerate a nil Side (used in pure unit tests).
	r := NewRegistry()
	RegisterBuiltins(r)
	for _, c := range r.List() {
		if _, err := c.Run(context.Background(), nil, nil); err != nil {
			t.Errorf("%s panicked/errored on nil side: %v", c.Name, err)
		}
	}
}
