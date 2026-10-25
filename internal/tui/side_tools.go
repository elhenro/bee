package tui

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/elhenro/bee/internal/commands"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/tools/browser"
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
	if s.m.engineBusy() {
		return errEngineBusy
	}
	cfg := &s.m.eng.Cfg
	switch {
	case !disabled:
		cfg.DisabledTools = slices.DeleteFunc(slices.Clone(cfg.DisabledTools), func(n string) bool { return n == name })
	case !slices.Contains(cfg.DisabledTools, name):
		cfg.DisabledTools = append(slices.Clone(cfg.DisabledTools), name)
	}
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
	if s.m.engineBusy() {
		return errEngineBusy
	}
	cfg := &s.m.eng.Cfg
	if slices.ContainsFunc(cfg.UserTools, func(u config.UserTool) bool { return u.Name == name }) {
		return fmt.Errorf("user tool %q already exists", name)
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
	return rebuildEngine(s.m.eng, "user tool")
}

// RemoveUserTool drops a [[user_tools]] entry, persists, and rebuilds the
// engine. Returns an error when the name is not a user tool — built-ins must
// be disabled via SetToolDisabled, not removed.
func (s *tuiSide) RemoveUserTool(name string) error {
	if s.m == nil || s.m.eng == nil {
		return errors.New("no tui state")
	}
	if s.m.engineBusy() {
		return errEngineBusy
	}
	cfg := &s.m.eng.Cfg
	isName := func(u config.UserTool) bool { return u.Name == name }
	if !slices.ContainsFunc(cfg.UserTools, isName) {
		return fmt.Errorf("no user tool %q", name)
	}
	cfg.UserTools = slices.DeleteFunc(slices.Clone(cfg.UserTools), isName)
	if err := PersistSetting("", "user_tools", cfg.UserTools); err != nil {
		return err
	}
	return rebuildEngine(s.m.eng, "user tool")
}

// OpenToolsPane flips a sentinel that Model.Update consumes to display the
// tools toggle pane. Errors in headless contexts.
func (s *tuiSide) OpenToolsPane() error {
	return s.armPane("tools pane", func(m *Model) bool { return m.toolsPane != nil }, func(m *Model) { m.toolsRequested = true })
}

// SetBrowserEnabled flips browser-tool support for this session only (no
// persist) and rebuilds the registry so the tools are dispatchable now.
// Turning on requires a detectable Chrome/Chromium. Returns a status line.
func (s *tuiSide) SetBrowserEnabled(on bool) (string, error) {
	if s.m == nil || s.m.eng == nil {
		return "", errors.New("no tui state")
	}
	cfg := &s.m.eng.Cfg
	if on {
		if _, err := browser.DetectChrome(cfg.Browser.ChromePath); err != nil {
			return "", fmt.Errorf("browser: %w (install Chrome/Chromium or set [browser] chrome_path)", err)
		}
	}
	prev := cfg.Browser.Enabled
	if prev == on {
		if on {
			return "browser tools already enabled", nil
		}
		return "browser tools already disabled", nil
	}
	cfg.Browser.Enabled = on
	if err := rebuildEngine(s.m.eng, "browser"); err != nil {
		cfg.Browser.Enabled = prev // revert on failure
		return "", err
	}
	if on {
		n := browserToolCount(s.m.eng)
		return fmt.Sprintf("browser tools enabled (%d tools). use /tools to fine-tune.", n), nil
	}
	return "browser tools disabled", nil
}

// browserToolCount counts registered tools whose name starts with "browser_".
func browserToolCount(e *loop.Engine) int {
	if e == nil || e.Tools == nil {
		return 0
	}
	n := 0
	for _, name := range e.Tools.Names() {
		if strings.HasPrefix(name, "browser_") {
			n++
		}
	}
	return n
}
