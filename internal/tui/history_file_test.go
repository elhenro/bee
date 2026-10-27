package tui

import (
	"fmt"
	"os"
	"runtime"
	"testing"
)

func smallHistory(t *testing.T, max int) string {
	t.Helper()
	t.Setenv("BEE_HOME", t.TempDir())
	old := historyMax
	historyMax = max
	t.Cleanup(func() { historyMax = old })
	return historyPath()
}

func TestAppendHistory_TrimsToCap(t *testing.T) {
	p := smallHistory(t, 5)
	for i := range 12 {
		AppendHistory(fmt.Sprintf("prompt %d", i))
	}
	lines := readHistoryLines(p)
	if len(lines) > 5 || lines[len(lines)-1] != "prompt 11" {
		t.Fatalf("history = %q, want at most 5 newest", lines)
	}
	if got := LoadHistory()[0]; got != "prompt 11" {
		t.Errorf("newest = %q", got)
	}
}

// short real prompts must still trim: the old size guard assumed 8 bytes per
// line and never fired for files this small.
func TestAppendHistory_TrimsShortLines(t *testing.T) {
	p := smallHistory(t, 100)
	for range 150 {
		AppendHistory("x")
	}
	if n := len(readHistoryLines(p)); n > 100 {
		t.Fatalf("%d lines kept, cap is 100", n)
	}
}

// the count is taken once per process, then tracked: a later append must not
// need a rescan, and a file that already exceeds the cap trims on first use.
func TestAppendHistory_CountsExistingFile(t *testing.T) {
	p := smallHistory(t, 3)
	if err := os.WriteFile(p, []byte("a\nb\nc\nd\ne\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	AppendHistory("f")
	if got := readHistoryLines(p); len(got) != 3 || got[2] != "f" {
		t.Fatalf("history = %q", got)
	}
}

func TestAppendHistory_OwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows reports 0666 for every writable file")
	}
	p := smallHistory(t, 100)
	AppendHistory("secret token")
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("new file mode = %v, %v; want 0600", fi.Mode().Perm(), err)
	}

	// a world-readable file from an older version is tightened on first use
	t.Setenv("BEE_HOME", t.TempDir())
	p = historyPath()
	if err := os.WriteFile(p, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	AppendHistory("new")
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("existing file mode = %v, want 0600", fi.Mode().Perm())
	}
}
