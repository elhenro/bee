package tui

import (
	"errors"

	"github.com/elhenro/bee/internal/config"
)

// boolSetting binds one persisted on/off setting to its Model field, its
// engine config field and, when the transcript renderer caches it, a live
// hook. Setting one updates all three and writes ~/.bee/config.toml.
type boolSetting struct {
	key  string
	def  bool // reported when there is no tui state
	ptrs func(*Model, *config.Config) (model, cfg *bool)
	live func(*StreamRenderer, bool)
}

var (
	flagVerbose         = boolSetting{"verbose", false, func(m *Model, c *config.Config) (*bool, *bool) { return &m.verbose, &c.Verbose }, (*StreamRenderer).SetVerbose}
	flagShowThoughts    = boolSetting{"show_thoughts", true, func(m *Model, c *config.Config) (*bool, *bool) { return &m.showThoughts, &c.ShowThoughts }, (*StreamRenderer).SetShowThoughts}
	flagShowNudges      = boolSetting{"show_nudges", false, func(m *Model, c *config.Config) (*bool, *bool) { return &m.showNudges, &c.ShowNudges }, (*StreamRenderer).SetShowNudges}
	flagShowRecap       = boolSetting{"show_recap", false, func(m *Model, c *config.Config) (*bool, *bool) { return &m.showRecap, &c.ShowRecap }, nil}
	flagCompact         = boolSetting{"compact", false, func(m *Model, c *config.Config) (*bool, *bool) { return &m.compact, &c.Compact }, (*StreamRenderer).SetCompact}
	flagShowContextBar  = boolSetting{"show_context_bar", false, func(m *Model, c *config.Config) (*bool, *bool) { return &m.showContextBar, &c.ShowContextBar }, nil}
	flagShowCallDetail  = boolSetting{"show_call_detail", false, func(m *Model, c *config.Config) (*bool, *bool) { return &m.showCallDetail, &c.ShowCallDetail }, (*StreamRenderer).SetShowCallDetail}
	flagHighlight       = boolSetting{"highlight", true, func(m *Model, c *config.Config) (*bool, *bool) { return &m.highlight, &c.Highlight }, (*StreamRenderer).SetHighlight}
	flagShellBangSilent = boolSetting{"shell_bang_silent", true, func(m *Model, c *config.Config) (*bool, *bool) { return &m.shellBangSilent, &c.ShellBangSilent }, nil}
	flagShowBanner      = boolSetting{"show_banner", true, func(m *Model, c *config.Config) (*bool, *bool) { return &m.showBanner, &c.ShowBanner }, nil}
	flagShowLoader      = boolSetting{"show_loader", true, func(m *Model, c *config.Config) (*bool, *bool) { return &m.showLoader, &c.ShowLoader }, (*StreamRenderer).SetShowLoader}

	// top-bar chrome and loader readout figures
	flagShowBee         = boolSetting{"show_bee", true, func(m *Model, c *config.Config) (*bool, *bool) { return &m.showBee, &c.ShowBee }, nil}
	flagShowContextPct  = boolSetting{"show_context_pct", true, func(m *Model, c *config.Config) (*bool, *bool) { return &m.showContextPct, &c.ShowContextPct }, nil}
	flagShowModel       = boolSetting{"show_model", true, func(m *Model, c *config.Config) (*bool, *bool) { return &m.showModel, &c.ShowModel }, nil}
	flagShowCwd         = boolSetting{"show_cwd", true, func(m *Model, c *config.Config) (*bool, *bool) { return &m.showCwd, &c.ShowCwd }, nil}
	flagShowEffort      = boolSetting{"show_effort", true, func(m *Model, c *config.Config) (*bool, *bool) { return &m.showEffort, &c.ShowEffort }, nil}
	flagShowTurnTimer   = boolSetting{"show_turn_timer", true, func(m *Model, c *config.Config) (*bool, *bool) { return &m.showTurnTimer, &c.ShowTurnTimer }, nil}
	flagShowGitBranch   = boolSetting{"show_git_branch", false, func(m *Model, c *config.Config) (*bool, *bool) { return &m.showGitBranch, &c.ShowGitBranch }, nil}
	flagShowTotalTokens = boolSetting{"show_total_tokens", false, func(m *Model, c *config.Config) (*bool, *bool) { return &m.showTotalTokens, &c.ShowTotalTokens }, nil}
	flagShowLoaderIn    = boolSetting{"show_loader_in", true, func(m *Model, c *config.Config) (*bool, *bool) { return &m.showLoaderIn, &c.ShowLoaderIn }, nil}
	flagShowLoaderOut   = boolSetting{"show_loader_out", true, func(m *Model, c *config.Config) (*bool, *bool) { return &m.showLoaderOut, &c.ShowLoaderOut }, nil}
	flagShowLoaderRate  = boolSetting{"show_loader_rate", true, func(m *Model, c *config.Config) (*bool, *bool) { return &m.showLoaderRate, &c.ShowLoaderRate }, nil}
)

// boolSettings lists every persisted on/off setting once: the settings pane
// snapshot, its toggle handler and the engine seed all iterate it.
var boolSettings = []boolSetting{
	flagVerbose, flagShowThoughts, flagShowNudges, flagShowRecap, flagCompact,
	flagShowContextBar, flagShowCallDetail, flagHighlight, flagShellBangSilent,
	flagShowBanner, flagShowLoader, flagShowBee, flagShowContextPct, flagShowModel,
	flagShowCwd, flagShowEffort, flagShowTurnTimer, flagShowGitBranch,
	flagShowTotalTokens, flagShowLoaderIn, flagShowLoaderOut, flagShowLoaderRate,
}

func flagByKey(key string) (boolSetting, bool) {
	for _, f := range boolSettings {
		if f.key == key {
			return f, true
		}
	}
	return boolSetting{}, false
}

// value reads the setting off the model.
func (f boolSetting) value(m *Model) bool {
	mp, _ := f.ptrs(m, new(config.Config))
	return *mp
}

// setFlag applies v live (model, renderer, engine config) and persists it.
func (s *tuiSide) setFlag(f boolSetting, v bool) error {
	if s.m == nil {
		return errors.New(f.key + ": no tui state")
	}
	cfg := new(config.Config) // scratch sink when there is no engine
	if s.m.eng != nil {
		cfg = &s.m.eng.Cfg
	}
	mp, cp := f.ptrs(s.m, cfg)
	*mp, *cp = v, v
	if f.live != nil && s.m.stream != nil {
		f.live(s.m.stream, v)
	}
	return PersistSetting("", f.key, v)
}

func (s *tuiSide) getFlag(f boolSetting) bool {
	if s.m == nil {
		return f.def
	}
	return f.value(s.m)
}

// SetVerbose toggles the verbose tool-output flag.
func (s *tuiSide) SetVerbose(v bool) error { return s.setFlag(flagVerbose, v) }
func (s *tuiSide) GetVerbose() bool        { return s.getFlag(flagVerbose) }

// SetShowThoughts toggles chain-of-thought visibility.
func (s *tuiSide) SetShowThoughts(v bool) error { return s.setFlag(flagShowThoughts, v) }
func (s *tuiSide) GetShowThoughts() bool        { return s.getFlag(flagShowThoughts) }

// SetShowNudges toggles render of synthetic `[nudge]` recovery turns. Loop
// behavior is unaffected: the agent still sees these messages, only the
// scrollback row is hidden when off (default).
func (s *tuiSide) SetShowNudges(v bool) error { return s.setFlag(flagShowNudges, v) }
func (s *tuiSide) GetShowNudges() bool        { return s.getFlag(flagShowNudges) }

// SetShowRecap toggles post-turn one-line side-LLM recap generation. Off = no
// side call. Live-applied so the next turn's onTurnDone uses the new value.
func (s *tuiSide) SetShowRecap(v bool) error { return s.setFlag(flagShowRecap, v) }
func (s *tuiSide) GetShowRecap() bool        { return s.getFlag(flagShowRecap) }

// SetCompact toggles compact TUI mode, which strips the spacing layer (gutter,
// inter-turn blank, bg-tint, OSC 133).
func (s *tuiSide) SetCompact(v bool) error { return s.setFlag(flagCompact, v) }
func (s *tuiSide) GetCompact() bool        { return s.getFlag(flagCompact) }

// SetShowContextBar toggles the bottom context-fill strip.
func (s *tuiSide) SetShowContextBar(v bool) error { return s.setFlag(flagShowContextBar, v) }
func (s *tuiSide) GetShowContextBar() bool        { return s.getFlag(flagShowContextBar) }

// SetShowCallDetail toggles the full-call row under labeled tool calls.
func (s *tuiSide) SetShowCallDetail(v bool) error { return s.setFlag(flagShowCallDetail, v) }
func (s *tuiSide) GetShowCallDetail() bool        { return s.getFlag(flagShowCallDetail) }

// SetHighlight toggles chroma syntax-highlighting of tool result previews,
// edit/write diffs and bash command summaries.
func (s *tuiSide) SetHighlight(v bool) error { return s.setFlag(flagHighlight, v) }
func (s *tuiSide) GetHighlight() bool        { return s.getFlag(flagHighlight) }

// SetShellBangSilent flips the default bang-command behavior: true (default)
// keeps `!cmd` local, false forwards it to the LLM. `!!` always inverts
// whichever default is active.
func (s *tuiSide) SetShellBangSilent(v bool) error { return s.setFlag(flagShellBangSilent, v) }
func (s *tuiSide) GetShellBangSilent() bool        { return s.getFlag(flagShellBangSilent) }

// SetShowBanner toggles the startup intro animation + bee logo. Takes effect
// on the next launch (intro is one-shot).
func (s *tuiSide) SetShowBanner(v bool) error { return s.setFlag(flagShowBanner, v) }
func (s *tuiSide) GetShowBanner() bool        { return s.getFlag(flagShowBanner) }

// SetShowLoader toggles the streaming "generating" animation.
func (s *tuiSide) SetShowLoader(v bool) error { return s.setFlag(flagShowLoader, v) }
func (s *tuiSide) GetShowLoader() bool        { return s.getFlag(flagShowLoader) }

func (s *tuiSide) SetShowBee(v bool) error { return s.setFlag(flagShowBee, v) }
func (s *tuiSide) GetShowBee() bool        { return s.getFlag(flagShowBee) }

func (s *tuiSide) SetShowContextPct(v bool) error { return s.setFlag(flagShowContextPct, v) }
func (s *tuiSide) GetShowContextPct() bool        { return s.getFlag(flagShowContextPct) }

func (s *tuiSide) SetShowModel(v bool) error { return s.setFlag(flagShowModel, v) }
func (s *tuiSide) GetShowModel() bool        { return s.getFlag(flagShowModel) }

func (s *tuiSide) SetShowCwd(v bool) error { return s.setFlag(flagShowCwd, v) }
func (s *tuiSide) GetShowCwd() bool        { return s.getFlag(flagShowCwd) }

func (s *tuiSide) SetShowEffort(v bool) error { return s.setFlag(flagShowEffort, v) }
func (s *tuiSide) GetShowEffort() bool        { return s.getFlag(flagShowEffort) }

func (s *tuiSide) SetShowTurnTimer(v bool) error { return s.setFlag(flagShowTurnTimer, v) }
func (s *tuiSide) GetShowTurnTimer() bool        { return s.getFlag(flagShowTurnTimer) }

func (s *tuiSide) SetShowGitBranch(v bool) error { return s.setFlag(flagShowGitBranch, v) }
func (s *tuiSide) GetShowGitBranch() bool        { return s.getFlag(flagShowGitBranch) }

func (s *tuiSide) SetShowTotalTokens(v bool) error { return s.setFlag(flagShowTotalTokens, v) }
func (s *tuiSide) GetShowTotalTokens() bool        { return s.getFlag(flagShowTotalTokens) }

func (s *tuiSide) SetShowLoaderIn(v bool) error { return s.setFlag(flagShowLoaderIn, v) }
func (s *tuiSide) GetShowLoaderIn() bool        { return s.getFlag(flagShowLoaderIn) }

func (s *tuiSide) SetShowLoaderOut(v bool) error { return s.setFlag(flagShowLoaderOut, v) }
func (s *tuiSide) GetShowLoaderOut() bool        { return s.getFlag(flagShowLoaderOut) }

func (s *tuiSide) SetShowLoaderRate(v bool) error { return s.setFlag(flagShowLoaderRate, v) }
func (s *tuiSide) GetShowLoaderRate() bool        { return s.getFlag(flagShowLoaderRate) }

// SetOuterPadLR applies the live TUI padding wrap. Clamps to 0..8 so a bad
// config value (or a script-supplied one) can't collapse the live region.
// Mirrors the value into eng.Cfg for the engine path (which has its own
// builder; we keep them aligned so headless + TUI produce the same line
// widths when both run in the same process).
func (s *tuiSide) SetOuterPadLR(n int) error {
	if s.m == nil {
		return errors.New("outer_pad_lr: no tui state")
	}
	n = min(max(n, 0), outerPadMax)
	s.m.outerPadLR = n
	if s.m.eng != nil {
		s.m.eng.Cfg.OuterPadLR = n
	}
	return PersistSetting("", "outer_pad_lr", n)
}

func (s *tuiSide) GetOuterPadLR() int {
	if s.m == nil {
		return 0
	}
	return s.m.outerPadLR
}
