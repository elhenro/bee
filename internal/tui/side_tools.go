package tui

import (
	"errors"
	"fmt"
	"sort"

	"github.com/elhenro/bee/internal/commands"
	"github.com/elhenro/bee/internal/config"
)

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
