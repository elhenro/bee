package tui

import (
	"context"
	"errors"
	"fmt"

	"github.com/elhenro/bee/internal/config"
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
