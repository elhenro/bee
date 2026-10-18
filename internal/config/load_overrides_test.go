package config

import (
	"path/filepath"
	"testing"
)

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("BEE_CONFIG", filepath.Join(t.TempDir(), "absent.toml"))
	t.Setenv("BEE_HOME", t.TempDir())
	for _, k := range []string{"BEE_MODEL", "BEE_PROVIDER", "BEE_PROFILE", "BEE_CAVEMAN"} {
		t.Setenv(k, "")
	}
}

func TestLoadWith_ProviderOverrideResolvesKeyModelProfile(t *testing.T) {
	isolate(t)
	t.Setenv("GEMINI_API_KEY", "g-key")
	t.Setenv("OPENROUTER_API_KEY", "or-key")

	c, err := LoadWith(Overrides{Provider: "gemini"})
	if err != nil {
		t.Fatal(err)
	}
	if c.APIKey != "g-key" {
		t.Errorf("APIKey = %q, want the gemini key", c.APIKey)
	}
	if c.DefaultModel != Defaults().Providers["gemini"].DefaultModel {
		t.Errorf("DefaultModel = %q, want gemini default", c.DefaultModel)
	}
	if want := ResolveAutoProfileForProvider("gemini", c.DefaultModel); c.Profile != want {
		t.Errorf("Profile = %q, want %q", c.Profile, want)
	}
}

func TestLoadWith_ModelOverrideWins(t *testing.T) {
	isolate(t)
	t.Setenv("ANTHROPIC_API_KEY", "a-key")
	c, err := LoadWith(Overrides{Provider: "anthropic", Model: "claude-opus-4-1"})
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultModel != "claude-opus-4-1" || c.APIKey != "a-key" {
		t.Errorf("model=%q key=%q", c.DefaultModel, c.APIKey)
	}
}

func TestLoad_EnvProviderTakesProviderDefaultModel(t *testing.T) {
	isolate(t)
	t.Setenv("BEE_PROVIDER", "anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "a-key")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if want := Defaults().Providers["anthropic"].DefaultModel; c.DefaultModel != want {
		t.Errorf("DefaultModel = %q, want %q", c.DefaultModel, want)
	}
}

func TestConfigPath_HonorsBeeHome(t *testing.T) {
	t.Setenv("BEE_CONFIG", "")
	t.Setenv("BEE_HOME", "/tmp/iso-bee")
	if got := ConfigPath(); got != "/tmp/iso-bee/config.toml" {
		t.Errorf("ConfigPath = %q", got)
	}
}
