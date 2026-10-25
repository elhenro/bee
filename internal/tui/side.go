package tui

import (
	"context"
	"errors"
	"fmt"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/loop"
)

// compile-time assertion left implicit; the registry takes any commands.Side.

// tuiSide is the Model's implementation of commands.Side. It exposes only
// the slice of Model state that slash commands need to mutate. Methods that
// depend on later milestones (F4 compaction, clipboard, session-load) return
// a clear "not implemented" error so the command framework stays usable.
type tuiSide struct {
	m *Model
}

var errNoTUI = errors.New("no tui")

// arm flips a sentinel on the model that Model.Update consumes. Errors when
// there is no tui so the slash command can fall back to text.
func (s *tuiSide) arm(set func(*Model)) error {
	if s.m == nil {
		return errNoTUI
	}
	set(s.m)
	return nil
}

// armPane is arm for a pane that headless runs never build.
func (s *tuiSide) armPane(pane string, built func(*Model) bool, set func(*Model)) error {
	if s.m == nil {
		return errNoTUI
	}
	if !built(s.m) {
		return errors.New("no " + pane + " (headless)")
	}
	set(s.m)
	return nil
}

// resetView drops transient stream state so the next flush reprints the
// (swapped) transcript from the top.
func (m *Model) resetView() {
	m.partial = ""
	m.streamFlushed = ""
	m.streamFenceOpen = false
	m.pendingFlushedPrefix = ""
	m.lastErr = ""
	m.state = StateIdle
	m.printedCount = 0
}

// rebuildEngine reruns the engine's rebuild hook, if any, so a changed
// provider, tool set or model takes effect on the next turn.
func rebuildEngine(e *loop.Engine, what string) error {
	if e.Rebuild == nil {
		return nil
	}
	if err := e.Rebuild(e); err != nil {
		return fmt.Errorf("%s: rebuild: %w", what, err)
	}
	return nil
}

// Compact delegates to the engine-level compactor. Stats are discarded — the
// slash-command path in commands/builtins.go only needs success/failure. The
// TUI's async /compact handler calls Engine.Compact directly to capture stats.
func (s *tuiSide) Compact(ctx context.Context) error {
	if s.m == nil || s.m.eng == nil {
		return nil
	}
	_, _, err := s.m.eng.Compact(ctx)
	return err
}

// SwitchModel mutates the status-bar model label and the engine's default
// model so the next turn picks it up. Provider stays whatever was set.
func (s *tuiSide) SwitchModel(name string) error {
	if name == "" {
		return errors.New("model: empty name")
	}
	if s.m.eng != nil && s.m.engineBusy() {
		return errEngineBusy
	}
	s.m.model = name
	if s.m.eng != nil {
		s.m.eng.Cfg.DefaultModel = name
		// rebuild memory adapter so its cached model id matches the new
		// default; provider stays the same so no client swap needed, but
		// going through Rebuild keeps both paths consistent.
		return rebuildEngine(s.m.eng, "model")
	}
	return nil
}

// SwitchProviderModel sets both fields. Called by the picker on commit.
// Also re-resolves the active profile so a switch to/from a local provider
// (ollama / lmstudio) flips between tiny and the model-class profile, then
// invokes Engine.Rebuild so the next turn instantiates the new provider's
// HTTP client. Without the rebuild the engine kept streaming against the
// pre-switch backend (e.g. ollama after picking openrouter).
func (s *tuiSide) SwitchProviderModel(provider, model string) error {
	if provider == "" {
		return errors.New("model: empty provider")
	}
	if s.m.eng != nil && s.m.engineBusy() {
		return errEngineBusy
	}
	if s.m.eng != nil {
		s.m.eng.Cfg.DefaultProvider = provider
		if model != "" {
			s.m.eng.Cfg.DefaultModel = model
		}
		// re-resolve the profile when auto is in play. An explicit
		// tiny/normal/large choice is preserved.
		s.m.eng.Cfg = reapplyAutoProfile(s.m.eng.Cfg)
		if err := rebuildEngine(s.m.eng, "model"); err != nil {
			return err
		}
	}
	if model != "" {
		s.m.model = model
	}
	return nil
}

// reapplyAutoProfile re-runs ApplyProfile-style resolution for an in-flight
// config change. Differs from ApplyProfile: leaves a concretely-named profile
// alone and only re-resolves "auto"/empty. Safe to call repeatedly.
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
	return s.armPane("picker", func(m *Model) bool { return m.picker != nil }, func(m *Model) { m.pickerRequested = true })
}

// OpenHandoff arms the rescue flow: opens the same provider+model picker as
// /model AND marks the resulting pick to route into the handoff path instead
// of a plain swap. Returns an error in headless contexts (no picker built) so
// the slash command can fall back to text.
func (s *tuiSide) OpenHandoff() error {
	return s.armPane("picker", func(m *Model) bool { return m.picker != nil }, func(m *Model) {
		m.handoffRequested = true
		m.handoffActive = true
	})
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
	if s.m != nil {
		s.m.treeRequested = true
	}
	return nil
}

// OpenCost flips a sentinel for the cost monitor pane.
func (s *tuiSide) OpenCost() error {
	if s.m != nil {
		s.m.costRequested = true
	}
	return nil
}

// OpenUsage flips a sentinel for the historical usage pane. Returns an error
// when there is no live model (headless) so the caller falls back to text.
func (s *tuiSide) OpenUsage() error {
	return s.arm(func(m *Model) { m.usageRequested = true })
}

// UsageText renders the usage overview as plain text (headless fallback).
func (s *tuiSide) UsageText() string { return renderUsageText() }
