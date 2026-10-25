package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCreatesAndReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	for _, want := range []string{"first", "second, longer"} {
		if err := Write(path, []byte(want), 0o600); err != nil {
			t.Fatalf("Write: %v", err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("read back = %q, %v; want %q", got, err, want)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %v, want 0600", info.Mode().Perm())
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Errorf("want only the target file, got %d entries", len(ents))
	}
}

func TestWriteFailureLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	// target is a directory: the rename must fail and the temp file be removed
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Write(target, []byte("x"), 0o600); err == nil {
		t.Fatal("want error renaming over a directory")
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Errorf("want only the target dir, got %d entries", len(ents))
	}
}
