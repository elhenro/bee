package tui

import (
	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/goal"
)

// introState is the startup animation state, embedded in Model.
type introState struct {
	// intro animation — plays above the input bar without blocking. Frames
	// are built lazily on the first tick once width is known. introDone
	// keeps the space reserved after the last frame so the live region
	// doesn't shrink; renderIntro then draws a static "bee v<x>" placeholder.
	// introDoneFrame drives the post-intro pulse (two bold-flashes on "bee");
	// it ticks up until introPulseFrames then settles.
	introActive    bool
	introDone      bool
	introStyle     IntroStyle
	introFrames    []IntroFrame
	introIdx       int
	introDoneFrame int

	// showBanner mirrors cfg.ShowBanner. Persisted via /settings; takes
	// effect on the NEXT launch (intro plays once at startup).
	showBanner bool
}

// tutorialFlow is the first-run walkthrough state, embedded in Model.
type tutorialFlow struct {
	// tutorial drives the first-run interactive walkthrough — a faked,
	// LLM-free session that teaches input, tool calls, roles, and slash
	// commands. Zero value = inactive. Replayed via /tutorial.
	tutorial tutorialState
	// tutorialPending defers the welcome gate until the intro animation
	// settles (set at startup when intro will play; consumed in onIntroTick).
	tutorialPending bool
	// tutorialRequested is the /tutorial sentinel Model.Update consumes to
	// replay the walkthrough — mirror of loginRequested/settingsRequested.
	tutorialRequested bool
}

// goalLoop is the /goal completion-condition loop state, embedded in Model.
type goalLoop struct {
	// goal holds the active /goal completion-condition loop state. Zero value
	// = inactive. After each clean turn a fast model judges whether the
	// condition is met; not-met auto-submits a continuation.
	goal goal.State
	// goalGen bumps on each scheduled eval so a stale async verdict (from a
	// cleared/replaced goal) is ignored when it lands.
	goalGen int
	// goalJudging is true while the fast model rules on the last turn; the
	// live region shows a busy row so the gap before the next turn isn't blank.
	goalJudging bool
}

// costState is the usage tracker and its panes, embedded in Model.
type costState struct {
	// costs is the per-session usage/dollar accumulator shared with the
	// engine. Read for the top-bar total and the /cost monitor pane.
	costs *cost.Tracker
	// costPane is the Ctrl+Y modal — opens on demand, claims keys while open.
	costPane      *CostPane
	costRequested bool
	// usagePane is the /usage modal — historical cross-session usage overview.
	usagePane      *UsagePane
	usageRequested bool
	costFlash
}

// costFlash is the post-turn top-bar cost animation state, embedded in Model.
type costFlash struct {
	// costFlashFrame counts up while a brief post-turn animation plays in
	// the top bar: colour-cycling the dollar amount and showing the delta.
	// costFlashUntil holds the inclusive end frame (0 = no flash active).
	// costFlashDelta is the USD added by the most recent turn.
	// costPrevCalls tracks how many events were already in the tracker the
	// last time we looked — used to detect "a new turn just landed".
	costFlashFrame int
	costFlashUntil int
	costFlashDelta float64
	costPrevCalls  int
}

// handoffFlow is the /handoff stuck-agent rescue state, embedded in Model.
type handoffFlow struct {
	// handoff flow — /handoff reuses the picker but routes the pick into a
	// stuck-agent rescue: build a distilled brief on the small/fast model,
	// switch to the picked bigger model, drop the stuck transcript, continue.
	// handoffActive routes the next PickedMsg; handoffing drives the loader
	// while the brief builds. handoffStall preserves the last wedge/bail reason
	// across handleSubmit's StateError recovery (which clears lastErr) so a
	// following /handoff can still feed it into the brief.
	handoffRequested bool
	handoffActive    bool
	handoffing       bool
	handoffStall     string
}

// compactFlow is the async /compact state, embedded in Model.
type compactFlow struct {
	// compacting is true while an async /compact goroutine is running.
	// renderLive uses this to keep the loader animation alive while the
	// summarization LLM call streams, since state stays StateIdle.
	compacting bool

	// queuedMidCompact holds plain-text submits typed while compacting.
	// Drained by onCompactDone — the queued text is fed back through
	// submit() once the compacted history is in place. Slash commands and
	// shell-bangs are not queued (they're meta, weird to delay). Last
	// queued wins on rapid retyping.
	queuedMidCompact string
}

// updateState is the self-update prompt bookkeeping, embedded in Model.
type updateState struct {
	// updateSeenSession suppresses re-prompts for the remainder of this
	// session after the user picks "later". Re-checking still happens — the
	// gate is between probe + show, not between probe + skip.
	updateSeenSession bool
	// updateApplying flags an in-flight install subprocess so the user can't
	// trigger a second one before the first finishes.
	updateApplying bool
}
