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
	s.m.mirrorToEngine()
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
	s.m.mirrorToEngine()
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
	s.m.mirrorToEngine()
	return PersistSetting("", "yolo", on)
}

// GetYolo reports whether the auto-approve toggle is armed.
func (s *tuiSide) GetYolo() bool {
	return s.m != nil && s.m.yolo
}

// OpenRolePicker flips a sentinel that Model.Update consumes to display the
// role picker modal. Returns an error in headless contexts.
func (s *tuiSide) OpenRolePicker() error {
	return s.armPane("role pane", func(m *Model) bool { return m.rolePane != nil }, func(m *Model) { m.roleRequested = true })
}

// OpenEffortPicker flips a sentinel that Model.Update consumes to display the
// effort picker modal. Returns an error in headless contexts.
func (s *tuiSide) OpenEffortPicker() error {
	return s.armPane("effort pane", func(m *Model) bool { return m.effortPane != nil }, func(m *Model) { m.effortRequested = true })
}

// SetMaxIterations changes the per-Run iteration cap live and persists it.
// 0 = unlimited; negatives clamp to 0. Takes effect on the next Run/continue
// (the active loop captured its ceiling at Run start).
func (s *tuiSide) SetMaxIterations(n int) error {
	if s.m == nil {
		return errors.New("max_iterations: no tui state")
	}
	n = max(n, 0)
	if s.m.eng != nil {
		if s.m.engineBusy() {
			return errEngineBusy
		}
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

// OpenSettings flips a sentinel that Model.Update consumes to display the
// settings pane modal. Errors in headless contexts.
func (s *tuiSide) OpenSettings() error {
	return s.armPane("settings pane", func(m *Model) bool { return m.settingsPane != nil }, func(m *Model) { m.settingsRequested = true })
}

// OpenAgentView opens the bgreg-backed multi-bee pane. The TUI's Update
// loop drains openHiveMsg to invoke AgentView.Open + Init.
func (s *tuiSide) OpenAgentView() error {
	return s.armPane("agent view", func(m *Model) bool { return m.agentView != nil }, func(m *Model) { m.agentView.Open() })
}
