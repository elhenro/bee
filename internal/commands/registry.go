// Package commands implements a slash-command registry for the bee TUI.
//
// A Command is registered by name (without the leading "/"). When the user
// types a line that begins with "/", the TUI parses out "<name> [args...]",
// looks up the command, and calls Run. Each Run either:
//   - returns text (non-empty) — TUI prints it as if assistant said it,
//   - returns "" — Run already performed a side effect via Side.
//
// Side decouples commands from TUI/Engine internals: the TUI provides the
// concrete implementation that wires through to its own state.
package commands

import (
	"context"
	"sort"
	"sync"
)

// Side is the surface a command uses to affect the TUI/engine without
// hard-depending on either package. Implementations live in the TUI.
type Side interface {
	// Compact summarizes older turns to free context window space.
	Compact(ctx context.Context) error
	// SwitchModel changes the active model id.
	SwitchModel(name string) error
	// SwitchProviderModel sets both provider and model in one call.
	// Empty model is allowed (provider-only switch); empty provider is rejected.
	SwitchProviderModel(provider, model string) error
	// OpenPicker asks the TUI to display the provider+model picker.
	// Returns nil when the modal was scheduled; non-nil signals headless
	// fallback so the slash command can hint usage instead.
	OpenPicker() error
	// ListSessions returns recent session ids, newest first.
	ListSessions() ([]string, error)
	// OpenSession loads a previously-recorded session by id.
	OpenSession(id string) error
	// NewSession clears scrollback and starts a fresh session.
	NewSession() error
	// CopyLast copies the last assistant message to the system clipboard.
	CopyLast() error
	// Quit signals the TUI to exit.
	Quit()
	// OpenTree opens the session tree modal.
	OpenTree() error
	// OpenResume opens the interactive session-resume picker. Returns an
	// error in headless contexts so the caller can fall back to a text list.
	OpenResume() error
	// OpenCost opens the cost monitor modal.
	OpenCost() error
	// ForkSession forks a new session at fromMsgID (or entire session if empty).
	ForkSession(fromMsgID string) error
	// CloneSession clones the entire current session into a new one.
	CloneSession() error
	// Login runs the OAuth PKCE flow for a provider and persists the token.
	Login(ctx context.Context, provider string) error
	// Logout removes the stored OAuth token AND any stored api key file for
	// a provider. Treated as "forget everything I saved" by the UI.
	Logout(provider string) error
	// SaveAPIKey persists a static api key for a non-oauth provider to
	// ~/.bee/auth/<provider>.key (0600). The next config load picks it up
	// in resolveAPIKey when the EnvKey env var is unset.
	SaveAPIKey(provider, key string) error
	// LoginStatus reports the auth state of every configured provider,
	// sorted alphabetically. Used by /login (no args) to guide the user.
	LoginStatus() []ProviderAuth
	// OpenLogin asks the TUI to display the interactive login pane.
	// Returns nil error when the pane was scheduled; non-nil signals the
	// caller (slash command) to fall back to rendered text — useful for
	// headless contexts where no pane exists.
	OpenLogin() error
	// SetThinking changes the active reasoning-effort level
	// (off|low|medium|high). Empty input is rejected by the caller.
	SetThinking(level string) error
	// GetThinking returns the current reasoning-effort level.
	GetThinking() string
	// OpenEffortPicker asks the TUI to display the effort picker modal.
	// Returns nil when the modal was scheduled; non-nil signals headless.
	OpenEffortPicker() error
	// SetVerbose mutates the verbose tool-output flag. Persists to config.
	SetVerbose(v bool) error
	// GetVerbose returns the current verbose flag.
	GetVerbose() bool
	// SetShowThoughts mutates the show-agent-thoughts flag. Persists to config.
	SetShowThoughts(v bool) error
	// GetShowThoughts returns the current show-thoughts flag.
	GetShowThoughts() bool
	// SetShowNudges toggles the visibility of synthetic `[nudge]` recovery
	// turns the loop injects. Persists to config. Render-only — the loop
	// continues to inject these messages regardless.
	SetShowNudges(v bool) error
	// GetShowNudges returns the current show-nudges flag.
	GetShowNudges() bool
	// SetShowRecap toggles post-turn one-line side-LLM recap generation.
	// Persists to config. Off = no side call.
	SetShowRecap(v bool) error
	// GetShowRecap returns the current show-recap flag.
	GetShowRecap() bool
	// SetCompact toggles compact TUI mode (no gutter/spacing/tint). Persists to config.
	SetCompact(v bool) error
	// GetCompact returns the current compact flag.
	GetCompact() bool
	// SetShowContextBar toggles the bottom context-fill strip. Persists to config.
	SetShowContextBar(v bool) error
	// GetShowContextBar returns the current show-context-bar flag.
	GetShowContextBar() bool
	// SetHighlight toggles chroma syntax-highlighting across diff/file/bash
	// previews. Persists to config.
	SetHighlight(v bool) error
	// GetHighlight returns the current highlight flag.
	GetHighlight() bool
	// SetShellBangSilent flips the default behavior of `!cmd`. true = run
	// locally without forwarding; false = legacy forward-to-LLM. `!!` always
	// inverts. Persists to config.
	SetShellBangSilent(v bool) error
	// GetShellBangSilent returns the current bang-silent flag.
	GetShellBangSilent() bool
	// Top-bar chrome toggles. Each persists to config; flipping all five off
	// collapses the entire status row.
	SetShowBee(v bool) error
	GetShowBee() bool
	SetShowContextPct(v bool) error
	GetShowContextPct() bool
	SetShowModel(v bool) error
	GetShowModel() bool
	SetShowCwd(v bool) error
	GetShowCwd() bool
	SetShowEffort(v bool) error
	GetShowEffort() bool
	SetShowTurnTimer(v bool) error
	GetShowTurnTimer() bool
	SetShowGitBranch(v bool) error
	GetShowGitBranch() bool
	SetShowTotalTokens(v bool) error
	GetShowTotalTokens() bool
	// SetShowBanner toggles the startup intro animation + bee logo. Persists.
	// Takes effect on next launch (intro is one-shot).
	SetShowBanner(v bool) error
	GetShowBanner() bool
	// SetShowLoader toggles the streaming "generating" animation live + persists.
	SetShowLoader(v bool) error
	GetShowLoader() bool
	// OpenSettings asks the TUI to display the settings pane. Returns an
	// error in headless contexts so the slash command can fall back to text.
	OpenSettings() error
	// OpenAgentView opens the bgreg-backed multi-bee pane (Left arrow).
	// Returns an error in headless contexts.
	OpenAgentView() error
	// ListTools reports every known tool with its enabled state and source
	// (builtin vs user). Sorted by name. Used by /tools (no args).
	ListTools() []ToolInfo
	// SetToolDisabled adds or removes name from cfg.DisabledTools and
	// persists. The filter applies on the next turn (live).
	SetToolDisabled(name string, disabled bool) error
	// AddUserTool persists a new [[user_tools]] entry and registers it live.
	// Errors if name collides with an existing tool.
	AddUserTool(name, command, description string) error
	// RemoveUserTool drops a [[user_tools]] entry by name and unregisters it.
	// Errors if the name is not a user tool.
	RemoveUserTool(name string) error
	// OpenToolsPane asks the TUI to display the tools toggle pane. Returns
	// an error in headless contexts.
	OpenToolsPane() error
}

// ToolInfo summarizes one tool entry for /tools UI.
type ToolInfo struct {
	Name        string
	Description string
	Disabled    bool
	UserDefined bool
}

// ProviderAuth summarizes one provider's auth posture for /login UX.
type ProviderAuth struct {
	Name         string // provider id (e.g., "anthropic")
	HasOAuth     bool   // [providers.<n>.oauth] is configured
	EnvKey       string // env var that supplies a static key (e.g., OPENAI_API_KEY)
	EnvSet       bool   // EnvKey is set in the current environment
	TokenSaved   bool   // ~/.bee/auth/<n>.json exists
	KeySaved     bool   // ~/.bee/auth/<n>.key exists (set via /login)
	KeyOptional  bool   // provider runs unauthenticated if no key set (e.g. omlx)
	IsDefault    bool   // matches Cfg.DefaultProvider
}

// Command is a /name entry.
type Command struct {
	Name        string
	Description string
	// AllowDuringRun marks a command as safe to dispatch while the engine is
	// mid-stream. Read-only commands (pickers, /help, /cost, /tree) and
	// out-of-band ones (/agent, /attach) set this true so the user isn't
	// gated behind a long-running turn.
	AllowDuringRun bool
	// Run returns text to display as assistant output (non-empty) or
	// empty if the command performed a side effect via Side.
	Run func(ctx context.Context, args []string, side Side) (string, error)
}

// Registry maps name -> Command. Safe for concurrent reads/writes.
type Registry struct {
	mu sync.RWMutex
	m  map[string]Command
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{m: map[string]Command{}} }

// Register adds or overwrites a command by name.
func (r *Registry) Register(c Command) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[c.Name] = c
}

// Get returns the command and whether it exists.
func (r *Registry) Get(name string) (Command, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.m[name]
	return c, ok
}

// List returns all commands sorted by name.
func (r *Registry) List() []Command {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Command, 0, len(r.m))
	for _, c := range r.m {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
