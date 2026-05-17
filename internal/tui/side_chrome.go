package tui

import "errors"

// Top-bar chrome toggles. Each mutates the live model, syncs the cached
// Cfg used at next launch, and persists to ~/.bee/config.toml.

func (s *tuiSide) SetShowBee(v bool) error {
	if s.m == nil {
		return errors.New("show_bee: no tui state")
	}
	s.m.showBee = v
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowBee = v
	}
	return PersistSetting("", "show_bee", v)
}

func (s *tuiSide) GetShowBee() bool {
	if s.m == nil {
		return true
	}
	return s.m.showBee
}

func (s *tuiSide) SetShowContextPct(v bool) error {
	if s.m == nil {
		return errors.New("show_context_pct: no tui state")
	}
	s.m.showContextPct = v
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowContextPct = v
	}
	return PersistSetting("", "show_context_pct", v)
}

func (s *tuiSide) GetShowContextPct() bool {
	if s.m == nil {
		return true
	}
	return s.m.showContextPct
}

func (s *tuiSide) SetShowModel(v bool) error {
	if s.m == nil {
		return errors.New("show_model: no tui state")
	}
	s.m.showModel = v
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowModel = v
	}
	return PersistSetting("", "show_model", v)
}

func (s *tuiSide) GetShowModel() bool {
	if s.m == nil {
		return true
	}
	return s.m.showModel
}

func (s *tuiSide) SetShowCwd(v bool) error {
	if s.m == nil {
		return errors.New("show_cwd: no tui state")
	}
	s.m.showCwd = v
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowCwd = v
	}
	return PersistSetting("", "show_cwd", v)
}

func (s *tuiSide) GetShowCwd() bool {
	if s.m == nil {
		return true
	}
	return s.m.showCwd
}

func (s *tuiSide) SetShowEffort(v bool) error {
	if s.m == nil {
		return errors.New("show_effort: no tui state")
	}
	s.m.showEffort = v
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowEffort = v
	}
	return PersistSetting("", "show_effort", v)
}

func (s *tuiSide) GetShowEffort() bool {
	if s.m == nil {
		return true
	}
	return s.m.showEffort
}

func (s *tuiSide) SetShowTurnTimer(v bool) error {
	if s.m == nil {
		return errors.New("show_turn_timer: no tui state")
	}
	s.m.showTurnTimer = v
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowTurnTimer = v
	}
	return PersistSetting("", "show_turn_timer", v)
}

func (s *tuiSide) GetShowTurnTimer() bool {
	if s.m == nil {
		return true
	}
	return s.m.showTurnTimer
}

func (s *tuiSide) SetShowGitBranch(v bool) error {
	if s.m == nil {
		return errors.New("show_git_branch: no tui state")
	}
	s.m.showGitBranch = v
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowGitBranch = v
	}
	return PersistSetting("", "show_git_branch", v)
}

func (s *tuiSide) GetShowGitBranch() bool {
	if s.m == nil {
		return false
	}
	return s.m.showGitBranch
}

func (s *tuiSide) SetShowTotalTokens(v bool) error {
	if s.m == nil {
		return errors.New("show_total_tokens: no tui state")
	}
	s.m.showTotalTokens = v
	if s.m.eng != nil {
		s.m.eng.Cfg.ShowTotalTokens = v
	}
	return PersistSetting("", "show_total_tokens", v)
}

func (s *tuiSide) GetShowTotalTokens() bool {
	if s.m == nil {
		return false
	}
	return s.m.showTotalTokens
}
