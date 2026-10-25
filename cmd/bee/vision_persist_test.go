package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/elhenro/bee/internal/config"
)

func TestPersistVisionModel_KeepsOtherKeysAndRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("BEE_CONFIG", path)
	t.Setenv("OPENROUTER_API_KEY", "sk-test") // load rejects the default provider without its key, ci has none
	seed := "default_model = 'qwen2.5-coder-7b'\n\n[vision]\nmodel = 'llava'\nendpoint = 'http://localhost:1234/v1'\n"
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := persistVisionModel("gemma3:12b", true); err != nil {
		t.Fatal(err)
	}
	if err := persistVisionModel("qwen3-coder:30b", false); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "qwen2.5-coder-7b" || cfg.Vision.Model != "llava" || cfg.Vision.Endpoint != "http://localhost:1234/v1" {
		t.Errorf("other keys lost: model=%q vision=%+v", cfg.DefaultModel, cfg.Vision)
	}
	if v, ok := cfg.Vision.Models["gemma3:12b"]; !ok || !v {
		t.Errorf("gemma3:12b not saved true: %v", cfg.Vision.Models)
	}
	if v, ok := cfg.Vision.Models["qwen3-coder:30b"]; !ok || v {
		t.Errorf("qwen3-coder:30b not saved false: %v", cfg.Vision.Models)
	}
}

func TestPersistVisionModel_CreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.toml")
	t.Setenv("BEE_CONFIG", path)
	if err := persistVisionModel("m", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
