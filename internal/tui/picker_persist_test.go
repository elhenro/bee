package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestPersistSetting_WritesBoolean(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	initial := `default_provider = "openrouter"
verbose = false
`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PersistSetting(path, "verbose", true); err != nil {
		t.Fatalf("PersistSetting: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := toml.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["verbose"] != true {
		t.Errorf("verbose = %v, want true", got["verbose"])
	}
	if got["default_provider"] != "openrouter" {
		t.Errorf("default_provider clobbered: %v", got["default_provider"])
	}
}

func TestPersistSetting_EmptyKeyRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := PersistSetting(path, "", true); err == nil {
		t.Fatal("expected error on empty key")
	}
}

func TestPersistPick_CreatesFreshFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	if err := PersistPick(path, "openrouter", "deepseek/deepseek-v4-flash"); err != nil {
		t.Fatalf("PersistPick: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := toml.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["default_provider"] != "openrouter" {
		t.Errorf("default_provider = %v", got["default_provider"])
	}
	if got["default_model"] != "deepseek/deepseek-v4-flash" {
		t.Errorf("default_model = %v", got["default_model"])
	}
}

func TestPersistPick_PreservesExistingFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	initial := `default_provider = "old"
default_model = "old-m"
caveman = "lite"

[sandbox]
scope = "read-only"
approval = "untrusted"

[providers.openrouter]
base_url = "https://openrouter.ai/api/v1"
env_key = "OPENROUTER_API_KEY"
`
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := PersistPick(path, "openrouter", "claude-sonnet-4-6"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	var got map[string]any
	if err := toml.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["default_provider"] != "openrouter" {
		t.Errorf("provider = %v", got["default_provider"])
	}
	if got["default_model"] != "claude-sonnet-4-6" {
		t.Errorf("model = %v", got["default_model"])
	}
	if got["caveman"] != "lite" {
		t.Errorf("caveman field lost: %v", got["caveman"])
	}
	sandbox, ok := got["sandbox"].(map[string]any)
	if !ok {
		t.Fatalf("sandbox table missing or wrong shape: %T %v", got["sandbox"], got["sandbox"])
	}
	if sandbox["scope"] != "read-only" {
		t.Errorf("sandbox.scope lost: %v", sandbox["scope"])
	}
	providers, ok := got["providers"].(map[string]any)
	if !ok {
		t.Fatalf("providers table missing: %T", got["providers"])
	}
	if _, ok := providers["openrouter"].(map[string]any); !ok {
		t.Errorf("providers.openrouter table lost")
	}
}

func TestPersistPick_EmptyModelOnlyUpdatesProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	initial := "default_provider = \"old\"\ndefault_model = \"keep-me\"\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := PersistPick(path, "new", ""); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, `default_provider = 'new'`) && !strings.Contains(s, `default_provider = "new"`) {
		t.Errorf("provider not updated: %s", s)
	}
	if !strings.Contains(s, "keep-me") {
		t.Errorf("default_model unexpectedly lost: %s", s)
	}
}

func TestPersistPick_EmptyProviderRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := PersistPick(path, "", "m"); err == nil {
		t.Fatal("expected error for empty provider")
	}
}

func TestPersistPick_AtomicLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := PersistPick(path, "p", "m"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "config.toml" {
			t.Errorf("leftover file: %s", e.Name())
		}
	}
}
