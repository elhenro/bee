package tui

import (
	"path/filepath"
	"testing"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/loop"
)

// TestEffortLevels_IncludesMastermind guards the picker exposing the top tier.
func TestEffortLevels_IncludesMastermind(t *testing.T) {
	var found bool
	for _, e := range effortLevels {
		if e.value == "mastermind" {
			found = true
		}
	}
	if !found {
		t.Fatalf("effortLevels missing the mastermind row: %+v", effortLevels)
	}
}

// TestSetThinking_MastermindTier verifies selecting mastermind pins thinking to
// max + flips the hive flag, and that leaving for a plain tier clears it.
func TestSetThinking_MastermindTier(t *testing.T) {
	// redirect persistence to a throwaway file so the real config is untouched.
	t.Setenv("BEE_CONFIG", filepath.Join(t.TempDir(), "config.toml"))

	eng := &loop.Engine{Cfg: config.Defaults()}
	s := &tuiSide{m: &Model{eng: eng}}

	if err := s.SetThinking("mastermind"); err != nil {
		t.Fatalf("SetThinking(mastermind): %v", err)
	}
	if !s.GetMastermind() {
		t.Error("GetMastermind = false, want true after selecting mastermind")
	}
	if !eng.Cfg.Mastermind {
		t.Error("eng.Cfg.Mastermind = false, want true")
	}
	if eng.Cfg.Thinking != "max" {
		t.Errorf("eng.Cfg.Thinking = %q, want max", eng.Cfg.Thinking)
	}
	if got := s.GetThinking(); got != "mastermind" {
		t.Errorf("GetThinking = %q, want mastermind", got)
	}

	// switch to a normal tier — hive turns off, thinking follows the new level.
	if err := s.SetThinking("high"); err != nil {
		t.Fatalf("SetThinking(high): %v", err)
	}
	if s.GetMastermind() {
		t.Error("GetMastermind = true, want false after leaving for high")
	}
	if eng.Cfg.Mastermind {
		t.Error("eng.Cfg.Mastermind = true, want false")
	}
	if got := s.GetThinking(); got != "high" {
		t.Errorf("GetThinking = %q, want high", got)
	}
}
