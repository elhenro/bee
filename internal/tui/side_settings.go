package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/elhenro/bee/internal/llm"
)

// SetRole switches the agent role (worker|scout|queen) live and persists it.
// Each role bakes its own reasoning budget, mirrored into m.thinking + the
// engine config so the next turn resolves the same value. Unknown values are
// rejected so a typo doesn't silently downgrade to worker.
func (s *tuiSide) SetRole(role string) error {
	if s.m == nil {
		return errors.New("role: no tui state")
	}
	r := strings.ToLower(strings.TrimSpace(role))
	switch r {
	case "worker", "scout", "queen":
	default:
		return fmt.Errorf("unknown role %q (want worker|scout|queen)", role)
	}
	s.m.role = r
	s.m.thinking = roleThinking(r)
	if s.m.eng != nil {
		s.m.eng.Cfg.Role = r
		s.m.eng.Cfg.Thinking = s.m.thinking
	}
	return PersistSetting("", "role", r)
}

// GetRole returns the active role string.
func (s *tuiSide) GetRole() string {
	if s.m == nil {
		return "worker"
	}
	return s.m.role
}

// SetThinking pins the reasoning budget live, overriding the role-baked
// default. Mirrored into m.thinking (display) + eng.Cfg.Thinking (engine
// resolves it next turn). Unknown levels are rejected so a typo doesn't
// silently fall to off.
func (s *tuiSide) SetThinking(level string) error {
	if s.m == nil {
		return errors.New("thinking: no tui state")
	}
	raw := strings.ToLower(strings.TrimSpace(level))
	switch raw {
	case "off", "low", "medium", "med", "high", "max", "maximum", "auto":
	default:
		return fmt.Errorf("unknown thinking %q (want off|low|medium|high|max|auto)", level)
	}
	canonical := string(llm.ParseThinking(raw))
	s.m.thinking = canonical
	if s.m.eng != nil {
		s.m.eng.Cfg.Thinking = canonical
	}
	return PersistSetting("", "thinking", canonical)
}

// GetThinking returns the active reasoning budget string.
func (s *tuiSide) GetThinking() string {
	if s.m == nil {
		return ""
	}
	return s.m.thinking
}

// SetYolo flips the auto-approve toggle live and persists it.
func (s *tuiSide) SetYolo(on bool) error {
	if s.m == nil {
		return errors.New("yolo: no tui state")
	}
	s.m.yolo = on
	if s.m.eng != nil {
		s.m.eng.Cfg.Yolo = on
	}
	return PersistSetting("", "yolo", on)
}

// GetYolo reports whether the auto-approve toggle is armed.
func (s *tuiSide) GetYolo() bool {
	return s.m != nil && s.m.yolo
}

// OpenRolePicker flips a sentinel that Model.Update consumes to display the
// role picker modal. Returns an error in headless contexts.
func (s *tuiSide) OpenRolePicker() error {
	if s.m == nil {
		return errors.New("no tui")
	}
	if s.m.rolePane == nil {
		return errors.New("no role pane (headless)")
	}
	s.m.roleRequested = true
	return nil
}

// OpenEffortPicker flips a sentinel that Model.Update consumes to display the
// effort picker modal. Returns an error in headless contexts.
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

// SetMaxIterations changes the per-Run iteration cap live and persists it.
// 0 = unlimited; negatives clamp to 0. Takes effect on the next Run/continue
// (the active loop captured its ceiling at Run start).
func (s *tuiSide) SetMaxIterations(n int) error {
	if s.m == nil {
		return errors.New("max_iterations: no tui state")
	}
	if n < 0 {
		n = 0
	}
	if s.m.eng != nil {
		s.m.eng.Cfg.MaxIterations = n
	}
	return PersistSetting("", "max_iterations", n)
}

// GetMaxIterations returns the current per-Run iteration cap (0 = unlimited).
func (s *tuiSide) GetMaxIterations() int {
	if s.m == nil || s.m.eng == nil {
		return 0
	}
	return s.m.eng.Cfg.MaxIterations
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

// SetShowRecap toggles post-turn one-line side-LLM recap generation and
// persists the choice. Off = no side call. Live-applied so the next
// turn's onTurnDone uses the new value without a relaunch.
func (s *tuiSide) SetShowRecap(v bool) error {
	if s.m == nil {
		return errors.New("show_recap: no tui state")
	}
	s.m.showRecap = v
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowRecap = v
	}
	return PersistSetting("", "show_recap", v)
}

// GetShowRecap returns the current show-recap flag.
func (s *tuiSide) GetShowRecap() bool {
	if s.m == nil {
		return false
	}
	return s.m.showRecap
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

// SetShowBanner toggles the startup intro animation + bee logo and persists.
// Takes effect on the next launch (intro is one-shot).
func (s *tuiSide) SetShowBanner(v bool) error {
	if s.m == nil {
		return errors.New("show_banner: no tui state")
	}
	s.m.showBanner = v
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowBanner = v
	}
	return PersistSetting("", "show_banner", v)
}

// GetShowBanner returns the current show-banner flag.
func (s *tuiSide) GetShowBanner() bool {
	if s.m == nil {
		return true
	}
	return s.m.showBanner
}

// SetShowLoader toggles the streaming "generating" animation live + persists.
func (s *tuiSide) SetShowLoader(v bool) error {
	if s.m == nil {
		return errors.New("show_loader: no tui state")
	}
	s.m.showLoader = v
	if s.m.stream != nil {
		s.m.stream.SetShowLoader(v)
	}
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowLoader = v
	}
	return PersistSetting("", "show_loader", v)
}

// GetShowLoader returns the current show-loader flag.
func (s *tuiSide) GetShowLoader() bool {
	if s.m == nil {
		return true
	}
	return s.m.showLoader
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
