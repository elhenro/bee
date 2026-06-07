package tui

import (
	"path/filepath"
	"testing"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/loop"
)

// TestRoleLevels_CoversAllRoles guards the picker exposing every role.
func TestRoleLevels_CoversAllRoles(t *testing.T) {
	want := map[string]bool{"worker": false, "scout": false, "queen": false}
	for _, e := range roleLevels {
		if _, ok := want[e.value]; ok {
			want[e.value] = true
		}
	}
	for v, found := range want {
		if !found {
			t.Errorf("roleLevels missing %q row: %+v", v, roleLevels)
		}
	}
}

// TestSetRole verifies switching role mirrors the baked thinking budget into
// the engine config and rejects unknown roles.
func TestSetRole(t *testing.T) {
	// redirect persistence to a throwaway file so the real config is untouched.
	t.Setenv("BEE_CONFIG", filepath.Join(t.TempDir(), "config.toml"))

	eng := &loop.Engine{Cfg: config.Defaults()}
	s := &tuiSide{m: &Model{eng: eng}}

	if err := s.SetRole("queen"); err != nil {
		t.Fatalf("SetRole(queen): %v", err)
	}
	if s.GetRole() != "queen" {
		t.Errorf("GetRole = %q, want queen", s.GetRole())
	}
	if eng.Cfg.Role != "queen" {
		t.Errorf("eng.Cfg.Role = %q, want queen", eng.Cfg.Role)
	}
	if eng.Cfg.Thinking != "max" { // queen bakes max
		t.Errorf("eng.Cfg.Thinking = %q, want max", eng.Cfg.Thinking)
	}

	if err := s.SetRole("scout"); err != nil {
		t.Fatalf("SetRole(scout): %v", err)
	}
	if eng.Cfg.Thinking != "high" { // scout bakes high
		t.Errorf("eng.Cfg.Thinking = %q, want high", eng.Cfg.Thinking)
	}

	if err := s.SetRole("bogus"); err == nil {
		t.Error("SetRole(bogus) should reject an unknown role")
	}
}

// TestSetThinking verifies pinning the reasoning budget mirrors into the
// engine config, canonicalizes aliases, and rejects unknown levels.
func TestSetThinking(t *testing.T) {
	t.Setenv("BEE_CONFIG", filepath.Join(t.TempDir(), "config.toml"))

	eng := &loop.Engine{Cfg: config.Defaults()}
	s := &tuiSide{m: &Model{eng: eng}}

	if err := s.SetThinking("high"); err != nil {
		t.Fatalf("SetThinking(high): %v", err)
	}
	if s.GetThinking() != "high" {
		t.Errorf("GetThinking = %q, want high", s.GetThinking())
	}
	if eng.Cfg.Thinking != "high" {
		t.Errorf("eng.Cfg.Thinking = %q, want high", eng.Cfg.Thinking)
	}

	if err := s.SetThinking("med"); err != nil { // alias → medium
		t.Fatalf("SetThinking(med): %v", err)
	}
	if eng.Cfg.Thinking != "medium" {
		t.Errorf("eng.Cfg.Thinking = %q, want medium", eng.Cfg.Thinking)
	}

	if err := s.SetThinking("bogus"); err == nil {
		t.Error("SetThinking(bogus) should reject an unknown level")
	}
}

// TestSetYolo flips the auto-approve toggle independent of role.
func TestSetYolo(t *testing.T) {
	t.Setenv("BEE_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	eng := &loop.Engine{Cfg: config.Defaults()}
	s := &tuiSide{m: &Model{eng: eng}}

	if err := s.SetYolo(true); err != nil {
		t.Fatalf("SetYolo(true): %v", err)
	}
	if !s.GetYolo() || !eng.Cfg.Yolo {
		t.Error("yolo should be armed on both model and engine config")
	}
	// switching role must not clear the yolo toggle.
	if err := s.SetRole("scout"); err != nil {
		t.Fatalf("SetRole: %v", err)
	}
	if !s.GetYolo() {
		t.Error("yolo toggle should survive a role switch")
	}
}
