package tui

import (
	"testing"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/loop"
)

// TestSwitchProviderModel_InvokesRebuild guards against the regression where
// /model + the picker mutated Cfg.DefaultProvider but the engine kept
// streaming against the stale Provider client (e.g. ollama after a switch
// to openrouter).
func TestSwitchProviderModel_InvokesRebuild(t *testing.T) {
	var calls int
	var sawProvider string
	var sawModel string
	eng := &loop.Engine{
		Cfg: config.Defaults(),
		Rebuild: func(e *loop.Engine) error {
			calls++
			sawProvider = e.Cfg.DefaultProvider
			sawModel = e.Cfg.DefaultModel
			return nil
		},
	}
	m := &Model{eng: eng}
	s := &tuiSide{m: m}

	if err := s.SwitchProviderModel("openrouter", "deepseek/deepseek-v4-flash"); err != nil {
		t.Fatalf("SwitchProviderModel: %v", err)
	}
	if calls != 1 {
		t.Fatalf("Rebuild calls = %d, want 1", calls)
	}
	if sawProvider != "openrouter" {
		t.Errorf("Rebuild saw provider = %q, want openrouter", sawProvider)
	}
	if sawModel != "deepseek/deepseek-v4-flash" {
		t.Errorf("Rebuild saw model = %q, want deepseek/deepseek-v4-flash", sawModel)
	}
	if eng.Cfg.DefaultProvider != "openrouter" {
		t.Errorf("eng.Cfg.DefaultProvider = %q, want openrouter", eng.Cfg.DefaultProvider)
	}
}

// TestSwitchModel_InvokesRebuild verifies the model-only path also rebuilds
// (memory adapter caches the model id used for the side-query LLM).
func TestSwitchModel_InvokesRebuild(t *testing.T) {
	var calls int
	eng := &loop.Engine{
		Cfg: config.Defaults(),
		Rebuild: func(*loop.Engine) error {
			calls++
			return nil
		},
	}
	s := &tuiSide{m: &Model{eng: eng}}
	if err := s.SwitchModel("gpt-4o"); err != nil {
		t.Fatalf("SwitchModel: %v", err)
	}
	if calls != 1 {
		t.Fatalf("Rebuild calls = %d, want 1", calls)
	}
	if eng.Cfg.DefaultModel != "gpt-4o" {
		t.Errorf("eng.Cfg.DefaultModel = %q, want gpt-4o", eng.Cfg.DefaultModel)
	}
}

// TestSwitchProviderModel_NoRebuildIsNoOp ensures headless / hive engines
// (which don't set Rebuild) don't crash on switch attempts.
func TestSwitchProviderModel_NoRebuildIsNoOp(t *testing.T) {
	eng := &loop.Engine{Cfg: config.Defaults()}
	s := &tuiSide{m: &Model{eng: eng}}
	if err := s.SwitchProviderModel("openrouter", "x"); err != nil {
		t.Fatalf("SwitchProviderModel: %v", err)
	}
}
