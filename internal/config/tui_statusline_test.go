package config

import "testing"

// The tui.status_line key must distinguish three states: absent (defaults),
// explicit empty (hide the line), and an ordered list. The loader preserves
// that distinction via a nil slice for "absent" and a non-nil empty slice for
// "[]", which the tui package reads to decide between defaults and hidden.
func TestTUIStatusLineConfig(t *testing.T) {
	t.Run("missing key is nil", func(t *testing.T) {
		dir := t.TempDir()
		writeConfig(t, dir, "default_provider = \"openrouter\"\n")
		c := Defaults()
		if err := mergeFile(&c, ConfigPath()); err != nil {
			t.Fatalf("merge: %v", err)
		}
		if c.TUI.StatusLine != nil {
			t.Fatalf("missing key should be nil, got %v", c.TUI.StatusLine)
		}
	})

	t.Run("empty array is non-nil empty", func(t *testing.T) {
		dir := t.TempDir()
		writeConfig(t, dir, "[tui]\nstatus_line = []\n")
		c := Defaults()
		if err := mergeFile(&c, ConfigPath()); err != nil {
			t.Fatalf("merge: %v", err)
		}
		if c.TUI.StatusLine == nil {
			t.Fatalf("explicit [] should be non-nil")
		}
		if len(c.TUI.StatusLine) != 0 {
			t.Fatalf("explicit [] should be empty, got %v", c.TUI.StatusLine)
		}
	})

	t.Run("ordered list preserved", func(t *testing.T) {
		dir := t.TempDir()
		writeConfig(t, dir, "[tui]\nstatus_line = [\"cwd\", \"model\", \"git_branch\"]\n")
		c := Defaults()
		if err := mergeFile(&c, ConfigPath()); err != nil {
			t.Fatalf("merge: %v", err)
		}
		want := []string{"cwd", "model", "git_branch"}
		if len(c.TUI.StatusLine) != len(want) {
			t.Fatalf("got %v, want %v", c.TUI.StatusLine, want)
		}
		for i := range want {
			if c.TUI.StatusLine[i] != want[i] {
				t.Fatalf("got %v, want %v", c.TUI.StatusLine, want)
			}
		}
	})
}
