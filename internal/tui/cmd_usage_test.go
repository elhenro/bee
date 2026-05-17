package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCmdUsage_BumpPersistsAndCounts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BEE_HOME", dir)

	u := loadCmdUsage()
	u.bump("help")
	u.bump("help")
	u.bump("compact")

	path := filepath.Join(dir, "cmd_usage.json")
	got := readUsageFile(t, path)
	if got["help"] != 2 || got["compact"] != 1 {
		t.Errorf("counts wrong: %+v", got)
	}

	u2 := loadCmdUsage()
	if u2.count("help") != 2 {
		t.Errorf("reload lost counts: %d", u2.count("help"))
	}
}

func TestCmdUsage_SortPaletteByUsage(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	u := loadCmdUsage()
	u.bump("model")
	u.bump("model")
	u.bump("help")

	pool := []PaletteEntry{
		{Name: "compact", Kind: EntryCommand},
		{Name: "help", Kind: EntryCommand},
		{Name: "model", Kind: EntryCommand},
		{Name: "agents", Kind: EntryCommand},
	}
	sortPaletteByUsage(pool, u)

	if pool[0].Name != "model" {
		t.Errorf("want model first (count=2), got %q", pool[0].Name)
	}
	if pool[1].Name != "help" {
		t.Errorf("want help second (count=1), got %q", pool[1].Name)
	}
	// zero-count entries: alpha order
	if pool[2].Name != "agents" || pool[3].Name != "compact" {
		t.Errorf("ties should sort alpha, got %q,%q", pool[2].Name, pool[3].Name)
	}
}

func TestCmdUsage_NilSafe(t *testing.T) {
	// loadCmdUsage never returns nil, but count/bump on a nil pointer must
	// not panic — they're called from a palette that may be zero-valued.
	var u *cmdUsage
	if u.count("x") != 0 {
		t.Error("nil count must return 0")
	}
	u.bump("x") // must not panic
}

func readUsageFile(t *testing.T, path string) map[string]int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]int{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
