package prompt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadContextFiles_WalksUp(t *testing.T) {
	root := t.TempDir()
	mid := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(mid, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("ROOT"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "CLAUDE.md"), []byte("MID"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mid, "AGENTS.md"), []byte("LEAF"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := LoadContextFiles(mid, "")
	wantOrder := []string{"ROOT", "MID", "LEAF"}
	if len(got) != 3 {
		t.Fatalf("want 3 files, got %d (%v)", len(got), got)
	}
	for i, w := range wantOrder {
		if got[i].Body != w {
			t.Errorf("idx %d: want %q got %q", i, w, got[i].Body)
		}
	}
}

func TestLoadContextFiles_StopsAtRoot(t *testing.T) {
	// passing the filesystem root must not loop or panic. with no beeHome
	// and no global AGENTS.md/CLAUDE.md likely at "/", the result should be
	// either empty or whatever the host has — never more than one entry,
	// since "/" has no parents to walk into.
	got := LoadContextFiles("/", "")
	if len(got) > 1 {
		t.Fatalf("root walk should yield at most 1 file, got %d (%v)", len(got), got)
	}
}

func TestLoadContextFiles_EmptyDir(t *testing.T) {
	// a tempdir with no AGENTS/CLAUDE at any level returns empty.
	dir := t.TempDir()
	got := LoadContextFiles(dir, "")
	if len(got) != 0 {
		t.Fatalf("want 0 files for empty dir, got %d (%v)", len(got), got)
	}
}

func TestLoadContextFiles_LoadsGlobal(t *testing.T) {
	beehome := t.TempDir()
	if err := os.WriteFile(filepath.Join(beehome, "AGENTS.md"), []byte("GLOBAL"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadContextFiles(t.TempDir(), beehome)
	if len(got) != 1 || got[0].Body != "GLOBAL" {
		t.Fatalf("want GLOBAL only, got %+v", got)
	}
}
