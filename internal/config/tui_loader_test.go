package config

import "testing"

// tui.loader names the busy indicator; absent means the default spinner row.
func TestTUILoaderConfig(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "default_provider = \"openrouter\"\n")
	c := Defaults()
	if err := mergeFile(&c, ConfigPath()); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if c.TUI.Loader != "" {
		t.Fatalf("absent key should be empty, got %q", c.TUI.Loader)
	}

	writeConfig(t, dir, "[tui]\nloader = \"strip\"\n")
	c = Defaults()
	if err := mergeFile(&c, ConfigPath()); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if c.TUI.Loader != "strip" {
		t.Fatalf("loader = %q, want strip", c.TUI.Loader)
	}
}
