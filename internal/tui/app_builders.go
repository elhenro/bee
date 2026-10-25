package tui

import (
	"slices"

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

// WithCompact seeds compact-mode rendering. Env/config-driven path.
func (m Model) WithCompact(v bool) Model {
	m.compact = v
	m.syncStream()
	return m
}

// WithShowLoader seeds the streaming-loader visibility. Config-driven path.
func (m Model) WithShowLoader(v bool) Model {
	m.showLoader = v
	m.syncStream()
	return m
}

// WithShowLoaderIn seeds the loader ↑ input-tokens figure visibility.
func (m Model) WithShowLoaderIn(v bool) Model { m.showLoaderIn = v; return m }

// WithShowLoaderOut seeds the loader ↓ output-tokens figure visibility.
func (m Model) WithShowLoaderOut(v bool) Model { m.showLoaderOut = v; return m }

// WithShowLoaderRate seeds the loader tok/s throughput figure visibility.
func (m Model) WithShowLoaderRate(v bool) Model { m.showLoaderRate = v; return m }

// WithStatusLine seeds the ordered status-line item list from config. nil
// keeps the built-in default order; a non-nil empty slice hides the line.
// The slice is copied so later mutation of the config can't leak in.
func (m Model) WithStatusLine(ids []string) Model {
	m.statusLine = slices.Clone(ids)
	return m
}

// WithOuterPadLR seeds the left+right outer-padding cell count around the
// live TUI region. Clamps to 0..8 so a tiny terminal can't collapse to a
// sliver or get a negative value from a typo.
func (m Model) WithOuterPadLR(n int) Model {
	m.outerPadLR = min(max(n, 0), 8)
	return m
}

// WithLoader seeds the busy-indicator style from config (tui.loader).
func (m Model) WithLoader(name string) Model {
	m.loader = name
	m.syncStream()
	return m
}

// WithStreamCh wires a text-delta channel from the engine into the TUI.
// The same channel must be set on Engine.StreamCh so deltas flow.
func (m Model) WithStreamCh(ch chan string) Model {
	m.streamCh = ch
	return m
}

// WithInitialMessages preloads scrollback. Used by `bee back <id>` to
// restore a prior session into the TUI on launch. The messages get
// flushed to terminal scrollback via tea.Println from Init().
func (m Model) WithInitialMessages(msgs []types.Message) Model {
	m.messages = append(m.messages, msgs...)
	return m
}

// WithSeedPrompt arms a one-shot auto-submit fired from Init. Used by
// skill dispatch to start a turn live in the TUI (e.g. "/research <topic>").
func (m Model) WithSeedPrompt(text string) Model {
	m.seedPrompt = text
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
