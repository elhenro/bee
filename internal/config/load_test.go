package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig drops a config.toml into dir and points BEE_CONFIG at it.
func writeConfig(t *testing.T, dir, body string) {
	t.Helper()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("BEE_CONFIG", path)
}

func TestLoad_MissingFileUsesDefaults(t *testing.T) {
	t.Setenv("BEE_CONFIG", filepath.Join(t.TempDir(), "absent.toml"))
	t.Setenv("OPENROUTER_API_KEY", "sk-test")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.DefaultProvider != "openrouter" {
		t.Errorf("DefaultProvider = %q", c.DefaultProvider)
	}
	if c.APIKey != "sk-test" {
		t.Errorf("APIKey = %q, want sk-test", c.APIKey)
	}
}

func TestLoad_FileOverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `
default_provider = "openai"
default_model = "gpt-5"
caveman = "lite"
profile = "large"

[sandbox]
scope = "read-only"
approval = "never"

[memory]
enabled = false
top_k = 0
`)
	t.Setenv("OPENAI_API_KEY", "sk-openai")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.DefaultProvider != "openai" {
		t.Errorf("DefaultProvider = %q, want openai", c.DefaultProvider)
	}
	if c.DefaultModel != "gpt-5" {
		t.Errorf("DefaultModel = %q", c.DefaultModel)
	}
	if c.Caveman != "lite" {
		t.Errorf("Caveman = %q", c.Caveman)
	}
	if c.Sandbox.Scope != "read-only" || c.Sandbox.Approval != "never" {
		t.Errorf("Sandbox = %+v", c.Sandbox)
	}
	if c.Memory.Enabled {
		t.Error("Memory.Enabled should be false from file")
	}
	if c.APIKey != "sk-openai" {
		t.Errorf("APIKey = %q", c.APIKey)
	}
}

func TestLoad_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `default_provider = "openai"
default_model = "gpt-5"
caveman = "lite"
profile = "large"
`)
	t.Setenv("BEE_PROVIDER", "openrouter")
	t.Setenv("BEE_MODEL", "x-ai/grok-4")
	t.Setenv("BEE_CAVEMAN", "ultra")
	t.Setenv("BEE_PROFILE", "tiny")
	t.Setenv("OPENROUTER_API_KEY", "sk-or")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.DefaultProvider != "openrouter" {
		t.Errorf("DefaultProvider = %q", c.DefaultProvider)
	}
	if c.DefaultModel != "x-ai/grok-4" {
		t.Errorf("DefaultModel = %q", c.DefaultModel)
	}
	if c.Caveman != "ultra" {
		t.Errorf("Caveman = %q", c.Caveman)
	}
	if c.Profile != "tiny" {
		t.Errorf("Profile = %q", c.Profile)
	}
}

func TestLoad_MissingAPIKeyErrors(t *testing.T) {
	t.Setenv("BEE_CONFIG", filepath.Join(t.TempDir(), "absent.toml"))
	os.Unsetenv("OPENROUTER_API_KEY")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing OPENROUTER_API_KEY")
	}
	if !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Errorf("error message should mention the env var; got %v", err)
	}
}

func TestLoad_LocalProviderNeedsNoKey(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `default_provider = "ollama"`)
	os.Unsetenv("OPENROUTER_API_KEY")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.APIKey != "" {
		t.Errorf("APIKey = %q, want empty for ollama", c.APIKey)
	}
}

func TestLoad_UnknownProviderErrors(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `default_provider = "nonsense"`)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "nonsense") {
		t.Errorf("error should name the missing provider; got %v", err)
	}
}

func TestConfigPath_EnvOverride(t *testing.T) {
	t.Setenv("BEE_CONFIG", "/tmp/custom-bee.toml")
	if got := ConfigPath(); got != "/tmp/custom-bee.toml" {
		t.Errorf("ConfigPath = %q", got)
	}
}
