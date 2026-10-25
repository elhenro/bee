package beehome

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.json")
	for _, body := range []string{"one", "two"} {
		if err := WriteAtomic(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(p); string(got) != body {
			t.Fatalf("content = %q, want %q", got, body)
		}
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
			t.Fatalf("perm = %v", fi.Mode().Perm())
		}
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Fatalf("temp files left behind: %v", ents)
	}
}

func TestWriteAtomicMissingDir(t *testing.T) {
	if err := WriteAtomic(filepath.Join(t.TempDir(), "no", "f"), nil, 0o644); err == nil {
		t.Fatal("want error for missing dir")
	}
}
