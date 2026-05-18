package tui

import (
	"os"

	"github.com/elhenro/bee/internal/commands"
	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/types"
)

// WithCostTracker wires the engine's cost.Tracker into the model so the
// top bar and the /cost pane can read it.
func (m Model) WithCostTracker(t *cost.Tracker) Model {
	m.costs = t
	return m
}

// WithApprover attaches the channel adapter that routes dangerous-command
// prompts through the TUI modal. The shell tool holds the same handle and
// blocks on Request until Resolve fires.
func (m Model) WithApprover(a *Approver) Model {
	m.approver = a
	return m
}

// WithIntro enables the non-blocking startup animation. Frames are built
// lazily on the first tick once width is known. BEE_NO_INTRO=1 disables.
func (m Model) WithIntro(style IntroStyle) Model {
	if os.Getenv("BEE_NO_INTRO") == "1" {
		return m
	}
	m.introStyle = style
	m.introActive = true
	return m
}

// WithVerbose seeds verbose tool-output rendering. CLI flag/env var path.
func (m Model) WithVerbose(v bool) Model {
	m.verbose = v
	if m.stream != nil {
		m.stream.SetVerbose(v)
	}
	return m
}

// WithShowThoughts seeds chain-of-thought visibility. Config-driven path.
func (m Model) WithShowThoughts(v bool) Model {
	m.showThoughts = v
	if m.stream != nil {
		m.stream.SetShowThoughts(v)
	}
	return m
}

// WithShowNudges seeds nudge-visibility from config. Default false hides
// the loop's [nudge] recovery turns; setting true reveals them.
func (m Model) WithShowNudges(v bool) Model {
	m.showNudges = v
	if m.stream != nil {
		m.stream.SetShowNudges(v)
	}
	return m
}

// WithShowRecap seeds the post-turn recap toggle from config. Default
// false so recaps are explicitly opt-in (extra tokens per turn).
func (m Model) WithShowRecap(v bool) Model {
	m.showRecap = v
	return m
}

// WithCompact seeds compact-mode rendering. Env/config-driven path.
func (m Model) WithCompact(v bool) Model {
	m.compact = v
	if m.stream != nil {
		m.stream.SetCompact(v)
	}
	return m
}

// WithShowContextBar seeds context-bar visibility. Config-driven path.
func (m Model) WithShowContextBar(v bool) Model {
	m.showContextBar = v
	return m
}

// WithHighlight seeds chroma syntax-highlighting state. Config-driven path.
func (m Model) WithHighlight(v bool) Model {
	m.highlight = v
	if m.stream != nil {
		m.stream.SetHighlight(v)
	}
	return m
}

// WithShellBangSilent seeds the bang default behavior. Config-driven path.
func (m Model) WithShellBangSilent(v bool) Model {
	m.shellBangSilent = v
	return m
}

// WithShowBanner seeds the intro-animation flag. Toggling at runtime only
// affects the NEXT launch — the startup animation is one-shot.
func (m Model) WithShowBanner(v bool) Model {
	m.showBanner = v
	return m
}

// WithShowLoader seeds the streaming-loader visibility. Config-driven path.
func (m Model) WithShowLoader(v bool) Model {
	m.showLoader = v
	if m.stream != nil {
		m.stream.SetShowLoader(v)
	}
	return m
}

// WithShowBee seeds top-bar bee-glyph visibility. Config-driven path.
func (m Model) WithShowBee(v bool) Model { m.showBee = v; return m }

// WithShowContextPct seeds top-bar percent-label visibility.
func (m Model) WithShowContextPct(v bool) Model { m.showContextPct = v; return m }

// WithShowModel seeds top-bar model-name visibility.
func (m Model) WithShowModel(v bool) Model { m.showModel = v; return m }

// WithShowCwd seeds top-bar cwd visibility.
func (m Model) WithShowCwd(v bool) Model { m.showCwd = v; return m }

// WithShowEffort seeds top-bar effort-badge visibility.
func (m Model) WithShowEffort(v bool) Model { m.showEffort = v; return m }

// WithShowTurnTimer seeds top-bar turn-timer chip visibility.
func (m Model) WithShowTurnTimer(v bool) Model { m.showTurnTimer = v; return m }

// WithShowGitBranch seeds top-bar git-branch chip visibility.
func (m Model) WithShowGitBranch(v bool) Model { m.showGitBranch = v; return m }

// WithShowTotalTokens seeds top-bar session-tokens chip visibility.
func (m Model) WithShowTotalTokens(v bool) Model { m.showTotalTokens = v; return m }

// WithStreamCh wires a text-delta channel from the engine into the TUI.
// The same channel must be set on Engine.StreamCh so deltas flow.
func (m Model) WithStreamCh(ch chan string) Model {
	m.streamCh = ch
	return m
}

// WithThinkCh wires a reasoning-delta channel from the engine into the TUI
// so chain-of-thought renders live during streaming. Same channel must be
// set on Engine.ThinkCh so deltas flow.
func (m Model) WithThinkCh(ch chan string) Model {
	m.thinkCh = ch
	return m
}

// WithLiveMsgCh wires a live-message channel from the engine into the TUI.
// The same channel must be set on Engine.LiveMsgCh so assistant + tool
// messages render as they're persisted, not only at Run completion.
func (m Model) WithLiveMsgCh(ch chan types.Message) Model {
	m.liveMsgCh = ch
	return m
}

// WithWarnCh wires the engine's transient-notice channel into the TUI so
// stream hiccups + retries surface as a fading line in chrome.
func (m Model) WithWarnCh(ch chan string) Model {
	m.warnCh = ch
	return m
}

// WithInitialMessages preloads scrollback. Used by `bee back <id>` to
// restore a prior session into the TUI on launch. The messages get
// flushed to terminal scrollback via tea.Println from Init().
func (m Model) WithInitialMessages(msgs []types.Message) Model {
	if len(msgs) == 0 {
		return m
	}
	m.messages = append(m.messages, msgs...)
	return m
}

// WithCommands swaps in a caller-provided registry. The palette is rebuilt
// against it so Ctrl+K shows the new set. Skills source is preserved.
func (m Model) WithCommands(r *commands.Registry) Model {
	if r == nil {
		return m
	}
	m.cmds = r
	m.palette = NewPalette(r, m.skills)
	return m
}

// WithSkills wires a skills source into the palette so the picker can list
// commands and skills side-by-side. *skills.Registry already satisfies
// SkillsLister, so callers usually pass their loaded registry directly.
func (m Model) WithSkills(sk SkillsLister) Model {
	m.skills = sk
	m.palette = NewPalette(m.cmds, sk)
	return m
}

// compile-time check: skills.Registry satisfies SkillsLister.
var _ SkillsLister = (*skills.Registry)(nil)

// WithKeyMap swaps in a caller-provided keymap. Used to fold in user
// overrides from ~/.bee/keybindings.json without changing NewModel's signature.
// Approval modal is rebuilt because it holds its own copy of the keys.
func (m Model) WithKeyMap(km KeyMap) Model {
	m.keys = km
	m.approval = NewApprovalModel(m.styles, km)
	return m
}
