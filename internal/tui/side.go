package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/elhenro/bee/internal/auth"
	"github.com/elhenro/bee/internal/commands"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/session"
	"github.com/google/uuid"
)

// compile-time assertion left implicit; the registry takes any commands.Side.

// tuiSide is the Model's implementation of commands.Side. It exposes only
// the slice of Model state that slash commands need to mutate. Methods that
// depend on later milestones (F4 compaction, clipboard, session-load) return
// a clear "not implemented" error so the command framework stays usable.
type tuiSide struct {
	m *Model
}

// Compact delegates to the engine-level compactor. Stats are discarded — the
// slash-command path in commands/builtins.go only needs success/failure. The
// TUI's async /compact handler calls Engine.Compact directly to capture stats.
func (s *tuiSide) Compact(ctx context.Context) error {
	if s.m == nil || s.m.eng == nil {
		return nil
	}
	_, err := s.m.eng.Compact(ctx)
	return err
}

// SwitchModel mutates the status-bar model label and the engine's default
// model so the next turn picks it up. Provider stays whatever was set.
func (s *tuiSide) SwitchModel(name string) error {
	if name == "" {
		return errors.New("model: empty name")
	}
	s.m.model = name
	if s.m.eng != nil {
		s.m.eng.Cfg.DefaultModel = name
		// rebuild memory adapter so its cached model id matches the new
		// default; provider stays the same so no client swap needed, but
		// going through Rebuild keeps both paths consistent.
		if s.m.eng.Rebuild != nil {
			if err := s.m.eng.Rebuild(s.m.eng); err != nil {
				return fmt.Errorf("model: rebuild: %w", err)
			}
		}
	}
	return nil
}

// SwitchProviderModel sets both fields. Called by the picker on commit.
// Also re-resolves the active profile so a switch to/from a local provider
// (ollama / lmstudio) flips between tiny and the model-class profile, then
// invokes Engine.Rebuild so the next turn instantiates the new provider's
// HTTP client. Without the rebuild the engine kept streaming against the
// pre-switch backend (e.g. ollama after picking openrouter).
// Switching to a local provider mid-session also forces Mode = "edit" when
// the session was sitting in "auto" — local models skip the classifier.
func (s *tuiSide) SwitchProviderModel(provider, model string) error {
	if provider == "" {
		return errors.New("model: empty provider")
	}
	if s.m.eng != nil {
		s.m.eng.Cfg.DefaultProvider = provider
		if model != "" {
			s.m.eng.Cfg.DefaultModel = model
		}
		// re-resolve profile + caveman when auto is in play. Explicit user
		// choices (tiny/normal/large or full/lite/ultra/off) are preserved.
		s.m.eng.Cfg = reapplyAutoProfile(s.m.eng.Cfg)
		if s.m.eng.Cfg.Mode == "auto" && config.IsLocalProvider(provider) {
			s.m.eng.Cfg.Mode = "edit"
			s.m.mode = "edit"
		}
		if s.m.eng.Rebuild != nil {
			if err := s.m.eng.Rebuild(s.m.eng); err != nil {
				return fmt.Errorf("model: rebuild: %w", err)
			}
		}
	}
	if model != "" {
		s.m.model = model
	}
	return nil
}

// reapplyAutoProfile re-runs ApplyProfile-style resolution for an in-flight
// config change. Differs from ApplyProfile: leaves a concretely-named profile
// alone but re-derives caveman if it was "auto"/empty originally. Safe to
// call repeatedly.
func reapplyAutoProfile(c config.Config) config.Config {
	if c.Profile == "auto" || c.Profile == "" {
		c.Profile = config.ResolveAutoProfileForProvider(c.DefaultProvider, c.DefaultModel)
	}
	return c
}

// OpenPicker flips a sentinel that Model.Update consumes to display the
// provider+model picker. Returns an error in headless contexts (no picker
// built) so the slash command can fall back to text.
func (s *tuiSide) OpenPicker() error {
	if s.m == nil {
		return errors.New("no tui")
	}
	if s.m.picker == nil {
		return errors.New("no picker (headless)")
	}
	s.m.pickerRequested = true
	return nil
}

// ListSessions enumerates rollouts on disk. Newest first by Created time
// is good enough for the resume picker; bee/internal/session.List already
// returns sessions with timestamps.
func (s *tuiSide) ListSessions() ([]string, error) {
	sess, err := session.List()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(sess))
	for _, x := range sess {
		out = append(out, x.ID)
	}
	return out, nil
}

// OpenSession swaps the engine to a previously-recorded rollout and seeds
// scrollback with its messages. The old rollout is closed; the new one is
// reopened in append mode so subsequent turns continue the conversation.
func (s *tuiSide) OpenSession(id string) error {
	if s.m == nil || s.m.eng == nil {
		return errors.New("no engine")
	}
	if id == "" {
		return errors.New("open session: empty id")
	}
	prior, err := session.Read(id)
	if err != nil {
		return err
	}
	roll, err := session.Open(id)
	if err != nil {
		return err
	}
	if s.m.eng.Sessions != nil {
		_ = s.m.eng.Sessions.Close()
	}
	s.m.eng.Sessions = roll
	s.m.messages = prior
	s.m.partial = ""
	s.m.streamFlushed = ""
	s.m.streamFenceOpen = false
	s.m.pendingFlushedPrefix = ""
	s.m.lastErr = ""
	s.m.state = StateIdle
	// Reset printedCount so flush() will emit the resumed transcript into
	// terminal scrollback. The slash-command caller (runSlash) calls flush
	// after Run() returns.
	s.m.printedCount = 0
	return nil
}

// OpenResume asks the TUI to display the interactive resume picker.
func (s *tuiSide) OpenResume() error {
	if s.m == nil {
		return errors.New("no tui")
	}
	s.m.resumeRequested = true
	return nil
}

// NewSession clears scrollback for a fresh conversation. Past content
// stays in the terminal scrollback (we can't unprint stdout); flush()
// resets its counter so future messages print correctly. Swaps the engine
// rollout to a fresh uuid and resets the cost tracker so the context-fill
// indicator drops back to 0% (without these, the next turn would re-send
// the prior conversation and LastInput would still reflect the old fill).
func (s *tuiSide) NewSession() error {
	if s.m == nil {
		return nil
	}
	s.m.messages = nil
	s.m.partial = ""
	s.m.streamFlushed = ""
	s.m.streamFenceOpen = false
	s.m.pendingFlushedPrefix = ""
	s.m.lastErr = ""
	s.m.state = StateIdle
	s.m.printedCount = 0
	if s.m.eng != nil {
		if s.m.eng.Sessions != nil {
			_ = s.m.eng.Sessions.Close()
		}
		roll, err := session.Open(uuid.NewString())
		if err != nil {
			return err
		}
		s.m.eng.Sessions = roll
		s.m.eng.InitialMessages = nil
		if s.m.eng.Costs != nil {
			s.m.eng.Costs.Reset()
		}
	}
	return nil
}

// CopyLast requires a clipboard dependency we haven't pulled in yet.
func (s *tuiSide) CopyLast() error {
	return errors.New("copy: not implemented yet")
}

// Quit flips a sentinel; Model.Update checks it on every tick and exits.
func (s *tuiSide) Quit() {
	s.m.quitRequested = true
}

// OpenTree flips a sentinel that Model.Update consumes to dispatch
// ToggleSessionTreeMsg on the next tick.
func (s *tuiSide) OpenTree() error {
	if s.m == nil {
		return nil
	}
	s.m.treeRequested = true
	return nil
}

// OpenCost flips a sentinel for the cost monitor pane.
func (s *tuiSide) OpenCost() error {
	if s.m == nil {
		return nil
	}
	s.m.costRequested = true
	return nil
}

// ForkSession forks the active session at fromMsgID (or fully if empty) and
// swaps the engine's rollout to the new one. Existing scrollback is cleared.
func (s *tuiSide) ForkSession(fromMsgID string) error {
	if s.m == nil || s.m.eng == nil || s.m.eng.Sessions == nil {
		return errors.New("no active session")
	}
	newR, err := session.Fork(s.m.eng.Sessions.ID(), fromMsgID)
	if err != nil {
		return err
	}
	_ = s.m.eng.Sessions.Close()
	s.m.eng.Sessions = newR
	s.m.messages = nil
	s.m.partial = ""
	s.m.streamFlushed = ""
	s.m.streamFenceOpen = false
	s.m.pendingFlushedPrefix = ""
	s.m.lastErr = ""
	s.m.state = StateIdle
	s.m.printedCount = 0
	return nil
}

// CloneSession is Fork with no message id — copies the full session.
func (s *tuiSide) CloneSession() error {
	return s.ForkSession("")
}

// Login runs the OAuth PKCE flow for the named provider. Provider must have
// an [oauth] block in config; otherwise this errors out. Token is persisted
// under ~/.bee/auth/<provider>.json with 0600 perms.
func (s *tuiSide) Login(ctx context.Context, provider string) error {
	if s.m == nil || s.m.eng == nil {
		return errors.New("login: no engine")
	}
	pcfg, ok := s.m.eng.Cfg.Providers[provider]
	if !ok {
		return fmt.Errorf("unknown provider %q", provider)
	}
	if pcfg.OAuth == nil {
		return fmt.Errorf("provider %q has no [oauth] config in ~/.bee/config.toml", provider)
	}
	dir, err := auth.DefaultDir()
	if err != nil {
		return err
	}
	if provider == "chatgpt" {
		fmt.Fprintln(os.Stderr, "note: /login chatgpt uses a public OpenAI first-party client_id against chatgpt.com.")
		fmt.Fprintln(os.Stderr, "      sanctioned for ChatGPT Plus/Pro/Team accounts; rate-limited per plan; may be revoked.")
	}
	tok, err := auth.Login(ctx, auth.LoginConfig{
		ClientID:             pcfg.OAuth.ClientID,
		AuthorizeEndpoint:    pcfg.OAuth.AuthorizeEndpoint,
		TokenEndpoint:        pcfg.OAuth.TokenEndpoint,
		Scope:                pcfg.OAuth.Scope,
		RedirectPath:         pcfg.OAuth.RedirectPath,
		RedirectPort:         pcfg.OAuth.RedirectPort,
		ExtraAuthorizeParams: pcfg.OAuth.ExtraAuthorizeParams,
		AccountIDClaim:       pcfg.OAuth.AccountIDClaim,
		Stdout:               os.Stderr,
	})
	if err != nil {
		return err
	}
	return auth.SaveToken(dir, provider, tok)
}

// LoginStatus enumerates configured providers and their auth posture
// (oauth configured, token saved, env key set, key file saved). Sorted
// alphabetically; the default provider keeps its position but is flagged
// IsDefault.
func (s *tuiSide) LoginStatus() []commands.ProviderAuth {
	if s.m == nil || s.m.eng == nil {
		return nil
	}
	cfg := s.m.eng.Cfg
	names := make([]string, 0, len(cfg.Providers))
	for n := range cfg.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	dir, _ := auth.DefaultDir()
	out := make([]commands.ProviderAuth, 0, len(names))
	for _, n := range names {
		p := cfg.Providers[n]
		entry := commands.ProviderAuth{
			Name:        n,
			HasOAuth:    p.OAuth != nil,
			EnvKey:      p.EnvKey,
			KeyOptional: p.KeyOptional,
			IsDefault:   n == cfg.DefaultProvider,
		}
		if p.EnvKey != "" {
			_, entry.EnvSet = os.LookupEnv(p.EnvKey)
		}
		if dir != "" {
			if tok, err := auth.LoadToken(dir, n); err == nil && tok != nil {
				entry.TokenSaved = true
			}
			entry.KeySaved = auth.HasAPIKey(dir, n)
		}
		out = append(out, entry)
	}
	return out
}

// OpenLogin flips a sentinel that Model.Update consumes to display the
// interactive login pane. Used by the no-arg /login slash command.
func (s *tuiSide) OpenLogin() error {
	if s.m == nil {
		return errors.New("no tui")
	}
	s.m.loginRequested = true
	return nil
}

// Logout removes both the stored OAuth token AND any stored api key file
// for the named provider. Either may be absent — both deletes are no-ops
// on ErrNotExist.
func (s *tuiSide) Logout(provider string) error {
	dir, err := auth.DefaultDir()
	if err != nil {
		return err
	}
	if err := auth.DeleteToken(dir, provider); err != nil {
		return err
	}
	return auth.DeleteAPIKey(dir, provider)
}

// SaveAPIKey persists a static api key for a non-oauth provider. Live
// engine config is updated too so the new key takes effect mid-session
// without a restart (when the saved provider matches the active one).
func (s *tuiSide) SaveAPIKey(provider, key string) error {
	if provider == "" {
		return errors.New("save key: empty provider")
	}
	dir, err := auth.DefaultDir()
	if err != nil {
		return err
	}
	if err := auth.SaveAPIKey(dir, provider, key); err != nil {
		return err
	}
	if s.m != nil && s.m.eng != nil && s.m.eng.Cfg.DefaultProvider == provider {
		s.m.eng.Cfg.APIKey = key
	}
	return nil
}

// SetThinking mutates the reasoning-effort level. Accepts the same strings
// as llm.ParseThinking (off|low|medium|high|max, "med" alias for medium);
// unknown values are rejected so silent typos don't downgrade to "off".
func (s *tuiSide) SetThinking(level string) error {
	if s.m == nil {
		return errors.New("effort: no tui state")
	}
	trimmed := level
	canonical := llm.ParseThinking(level)
	if canonical == llm.ThinkingOff && trimmed != "" && trimmed != "off" {
		return fmt.Errorf("unknown effort %q (want auto|off|low|medium|high|max)", level)
	}
	s.m.thinking = string(canonical)
	if s.m.eng != nil {
		s.m.eng.Cfg.Thinking = string(canonical)
	}
	return PersistSetting("", "thinking", string(canonical))
}

// GetThinking returns the current reasoning-effort level as a string.
func (s *tuiSide) GetThinking() string {
	if s.m == nil {
		return string(llm.ThinkingOff)
	}
	return s.m.thinking
}

// OpenEffortPicker flips a sentinel that Model.Update consumes to display
// the effort picker modal. Returns an error in headless contexts.
func (s *tuiSide) OpenEffortPicker() error {
	if s.m == nil {
		return errors.New("no tui")
	}
	if s.m.effortPane == nil {
		return errors.New("no effort pane (headless)")
	}
	s.m.effortRequested = true
	return nil
}

// SetVerbose mutates the verbose tool-output flag live and persists it to
// ~/.bee/config.toml so the next launch picks it up.
func (s *tuiSide) SetVerbose(v bool) error {
	if s.m == nil {
		return errors.New("verbose: no tui state")
	}
	s.m.verbose = v
	if s.m.stream != nil {
		s.m.stream.SetVerbose(v)
	}
	if s.m.eng != nil {
		s.m.eng.Cfg.Verbose = v
	}
	return PersistSetting("", "verbose", v)
}

// GetVerbose returns the current verbose flag.
func (s *tuiSide) GetVerbose() bool {
	if s.m == nil {
		return false
	}
	return s.m.verbose
}

// SetShowThoughts mutates the chain-of-thought visibility flag live and
// persists it to ~/.bee/config.toml.
func (s *tuiSide) SetShowThoughts(v bool) error {
	if s.m == nil {
		return errors.New("show_thoughts: no tui state")
	}
	s.m.showThoughts = v
	if s.m.stream != nil {
		s.m.stream.SetShowThoughts(v)
	}
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowThoughts = v
	}
	return PersistSetting("", "show_thoughts", v)
}

// GetShowThoughts returns the current show-thoughts flag.
func (s *tuiSide) GetShowThoughts() bool {
	if s.m == nil {
		return true
	}
	return s.m.showThoughts
}

// SetShowNudges toggles render of synthetic `[nudge]` recovery turns and
// persists the choice. Loop behavior is unaffected — the agent still sees
// these messages, only the scrollback row is hidden when off (default).
func (s *tuiSide) SetShowNudges(v bool) error {
	if s.m == nil {
		return errors.New("show_nudges: no tui state")
	}
	s.m.showNudges = v
	if s.m.stream != nil {
		s.m.stream.SetShowNudges(v)
	}
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowNudges = v
	}
	return PersistSetting("", "show_nudges", v)
}

// GetShowNudges returns the current show-nudges flag.
func (s *tuiSide) GetShowNudges() bool {
	if s.m == nil {
		return false
	}
	return s.m.showNudges
}

// SetCompact toggles compact TUI mode live and persists it. Compact strips
// the spacing layer (gutter, inter-turn blank, bg-tint, OSC 133).
func (s *tuiSide) SetCompact(v bool) error {
	if s.m == nil {
		return errors.New("compact: no tui state")
	}
	s.m.compact = v
	if s.m.stream != nil {
		s.m.stream.SetCompact(v)
	}
	if s.m.eng != nil {
		s.m.eng.Cfg.Compact = v
	}
	return PersistSetting("", "compact", v)
}

// GetCompact returns the current compact flag.
func (s *tuiSide) GetCompact() bool {
	if s.m == nil {
		return false
	}
	return s.m.compact
}

// SetShowContextBar toggles the bottom context-fill strip live + persists.
func (s *tuiSide) SetShowContextBar(v bool) error {
	if s.m == nil {
		return errors.New("show_context_bar: no tui state")
	}
	s.m.showContextBar = v
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowContextBar = v
	}
	return PersistSetting("", "show_context_bar", v)
}

// GetShowContextBar returns the current show-context-bar flag.
func (s *tuiSide) GetShowContextBar() bool {
	if s.m == nil {
		return false
	}
	return s.m.showContextBar
}

// SetHighlight toggles chroma syntax-highlighting live + persists. Affects
// tool result previews, edit/write diffs, bash command summaries.
func (s *tuiSide) SetHighlight(v bool) error {
	if s.m == nil {
		return errors.New("highlight: no tui state")
	}
	s.m.highlight = v
	if s.m.stream != nil {
		s.m.stream.SetHighlight(v)
	}
	if s.m.eng != nil {
		s.m.eng.Cfg.Highlight = v
	}
	return PersistSetting("", "highlight", v)
}

// GetHighlight returns the current highlight flag.
func (s *tuiSide) GetHighlight() bool {
	if s.m == nil {
		return true
	}
	return s.m.highlight
}

// SetShellBangSilent flips the default bang-command behavior live + persists.
// true (default) = `!cmd` stays local; false = legacy forward-to-LLM. `!!`
// always inverts whichever default is active.
func (s *tuiSide) SetShellBangSilent(v bool) error {
	if s.m == nil {
		return errors.New("shell_bang_silent: no tui state")
	}
	s.m.shellBangSilent = v
	if s.m.eng != nil {
		s.m.eng.Cfg.ShellBangSilent = v
	}
	return PersistSetting("", "shell_bang_silent", v)
}

// GetShellBangSilent returns the current bang-silent flag.
func (s *tuiSide) GetShellBangSilent() bool {
	if s.m == nil {
		return true
	}
	return s.m.shellBangSilent
}

// Top-bar chrome toggles. Each mutates the live model, syncs the cached
// Cfg used at next launch, and persists to ~/.bee/config.toml.

func (s *tuiSide) SetShowBee(v bool) error {
	if s.m == nil {
		return errors.New("show_bee: no tui state")
	}
	s.m.showBee = v
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowBee = v
	}
	return PersistSetting("", "show_bee", v)
}

func (s *tuiSide) GetShowBee() bool {
	if s.m == nil {
		return true
	}
	return s.m.showBee
}

func (s *tuiSide) SetShowContextPct(v bool) error {
	if s.m == nil {
		return errors.New("show_context_pct: no tui state")
	}
	s.m.showContextPct = v
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowContextPct = v
	}
	return PersistSetting("", "show_context_pct", v)
}

func (s *tuiSide) GetShowContextPct() bool {
	if s.m == nil {
		return true
	}
	return s.m.showContextPct
}

func (s *tuiSide) SetShowModel(v bool) error {
	if s.m == nil {
		return errors.New("show_model: no tui state")
	}
	s.m.showModel = v
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowModel = v
	}
	return PersistSetting("", "show_model", v)
}

func (s *tuiSide) GetShowModel() bool {
	if s.m == nil {
		return true
	}
	return s.m.showModel
}

func (s *tuiSide) SetShowCwd(v bool) error {
	if s.m == nil {
		return errors.New("show_cwd: no tui state")
	}
	s.m.showCwd = v
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowCwd = v
	}
	return PersistSetting("", "show_cwd", v)
}

func (s *tuiSide) GetShowCwd() bool {
	if s.m == nil {
		return true
	}
	return s.m.showCwd
}

func (s *tuiSide) SetShowEffort(v bool) error {
	if s.m == nil {
		return errors.New("show_effort: no tui state")
	}
	s.m.showEffort = v
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowEffort = v
	}
	return PersistSetting("", "show_effort", v)
}

func (s *tuiSide) GetShowEffort() bool {
	if s.m == nil {
		return true
	}
	return s.m.showEffort
}

func (s *tuiSide) SetShowTurnTimer(v bool) error {
	if s.m == nil {
		return errors.New("show_turn_timer: no tui state")
	}
	s.m.showTurnTimer = v
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowTurnTimer = v
	}
	return PersistSetting("", "show_turn_timer", v)
}

func (s *tuiSide) GetShowTurnTimer() bool {
	if s.m == nil {
		return true
	}
	return s.m.showTurnTimer
}

func (s *tuiSide) SetShowGitBranch(v bool) error {
	if s.m == nil {
		return errors.New("show_git_branch: no tui state")
	}
	s.m.showGitBranch = v
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowGitBranch = v
	}
	return PersistSetting("", "show_git_branch", v)
}

func (s *tuiSide) GetShowGitBranch() bool {
	if s.m == nil {
		return false
	}
	return s.m.showGitBranch
}

func (s *tuiSide) SetShowTotalTokens(v bool) error {
	if s.m == nil {
		return errors.New("show_total_tokens: no tui state")
	}
	s.m.showTotalTokens = v
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowTotalTokens = v
	}
	return PersistSetting("", "show_total_tokens", v)
}

func (s *tuiSide) GetShowTotalTokens() bool {
	if s.m == nil {
		return false
	}
	return s.m.showTotalTokens
}

// OpenSettings flips a sentinel that Model.Update consumes to display the
// settings pane modal. Errors in headless contexts.
func (s *tuiSide) OpenSettings() error {
	if s.m == nil {
		return errors.New("no tui")
	}
	if s.m.settingsPane == nil {
		return errors.New("no settings pane (headless)")
	}
	s.m.settingsRequested = true
	return nil
}

// OpenAgentView opens the bgreg-backed multi-bee pane. The TUI's Update
// loop drains openHiveMsg to invoke AgentView.Open + Init.
func (s *tuiSide) OpenAgentView() error {
	if s.m == nil {
		return errors.New("no tui")
	}
	if s.m.agentView == nil {
		return errors.New("no agent view (headless)")
	}
	s.m.agentView.Open()
	return nil
}

// ListTools returns every tool with disabled flag + user-defined source.
// Built-ins come from the live registry; user tools come from cfg.UserTools.
// A user tool present in cfg but absent from registry (e.g. disabled at
// build time) is still listed so the toggle pane can re-enable it.
func (s *tuiSide) ListTools() []commands.ToolInfo {
	if s.m == nil || s.m.eng == nil {
		return nil
	}
	cfg := s.m.eng.Cfg
	disabled := make(map[string]bool, len(cfg.DisabledTools))
	for _, n := range cfg.DisabledTools {
		disabled[n] = true
	}
	user := make(map[string]config.UserTool, len(cfg.UserTools))
	for _, u := range cfg.UserTools {
		user[u.Name] = u
	}
	seen := map[string]bool{}
	var out []commands.ToolInfo
	if s.m.eng.Tools != nil {
		for _, spec := range s.m.eng.Tools.Specs() {
			_, isUser := user[spec.Name]
			out = append(out, commands.ToolInfo{
				Name:        spec.Name,
				Description: spec.Description,
				Disabled:    disabled[spec.Name],
				UserDefined: isUser,
			})
			seen[spec.Name] = true
		}
	}
	// surface disabled-only entries that aren't in the registry
	for name := range disabled {
		if seen[name] {
			continue
		}
		desc := ""
		userDef := false
		if u, ok := user[name]; ok {
			desc = u.Description
			userDef = true
		}
		out = append(out, commands.ToolInfo{Name: name, Description: desc, Disabled: true, UserDefined: userDef})
		seen[name] = true
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// SetToolDisabled updates cfg.DisabledTools and persists. Loop turn.go filters
// the spec list live on the next turn so the change is visible without rebuild.
func (s *tuiSide) SetToolDisabled(name string, disabled bool) error {
	if s.m == nil || s.m.eng == nil {
		return errors.New("no tui state")
	}
	if name == "" {
		return errors.New("empty tool name")
	}
	cfg := &s.m.eng.Cfg
	cur := cfg.DisabledTools[:0:0]
	dup := false
	for _, n := range cfg.DisabledTools {
		if n == name {
			dup = true
			if disabled {
				cur = append(cur, n)
			}
			continue
		}
		cur = append(cur, n)
	}
	if disabled && !dup {
		cur = append(cur, name)
	}
	cfg.DisabledTools = cur
	return PersistSetting("", "disabled_tools", cfg.DisabledTools)
}

// AddUserTool registers a new user shell-alias tool live and persists it.
// Name collisions with existing tools (builtins or other user tools) are
// rejected. Empty name/cmd are rejected.
func (s *tuiSide) AddUserTool(name, cmdStr, desc string) error {
	if s.m == nil || s.m.eng == nil {
		return errors.New("no tui state")
	}
	if name == "" || cmdStr == "" {
		return errors.New("name and command required")
	}
	cfg := &s.m.eng.Cfg
	for _, u := range cfg.UserTools {
		if u.Name == name {
			return fmt.Errorf("user tool %q already exists", name)
		}
	}
	if s.m.eng.Tools != nil {
		if _, ok := s.m.eng.Tools.Get(name); ok {
			return fmt.Errorf("tool %q already registered", name)
		}
	}
	cfg.UserTools = append(cfg.UserTools, config.UserTool{Name: name, Command: cmdStr, Description: desc})
	if err := PersistSetting("", "user_tools", cfg.UserTools); err != nil {
		return err
	}
	// rebuild the engine's tool registry so the new tool is dispatchable now
	if s.m.eng.Rebuild != nil {
		if err := s.m.eng.Rebuild(s.m.eng); err != nil {
			return fmt.Errorf("user tool: rebuild: %w", err)
		}
	}
	return nil
}

// RemoveUserTool drops a [[user_tools]] entry, persists, and rebuilds the
// engine. Returns an error when the name is not a user tool — built-ins must
// be disabled via SetToolDisabled, not removed.
func (s *tuiSide) RemoveUserTool(name string) error {
	if s.m == nil || s.m.eng == nil {
		return errors.New("no tui state")
	}
	cfg := &s.m.eng.Cfg
	found := false
	out := cfg.UserTools[:0:0]
	for _, u := range cfg.UserTools {
		if u.Name == name {
			found = true
			continue
		}
		out = append(out, u)
	}
	if !found {
		return fmt.Errorf("no user tool %q", name)
	}
	cfg.UserTools = out
	if err := PersistSetting("", "user_tools", cfg.UserTools); err != nil {
		return err
	}
	if s.m.eng.Rebuild != nil {
		if err := s.m.eng.Rebuild(s.m.eng); err != nil {
			return fmt.Errorf("user tool: rebuild: %w", err)
		}
	}
	return nil
}

// OpenToolsPane flips a sentinel that Model.Update consumes to display the
// tools toggle pane. Errors in headless contexts.
func (s *tuiSide) OpenToolsPane() error {
	if s.m == nil {
		return errors.New("no tui")
	}
	if s.m.toolsPane == nil {
		return errors.New("no tools pane (headless)")
	}
	s.m.toolsRequested = true
	return nil
}
