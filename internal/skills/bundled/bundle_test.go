package bundled

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestFiles_haveAllFour(t *testing.T) {
	names, err := Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	want := []string{"calc.md", "caveman-review.md", "caveman-commit.md", "hermes.md"}
	for _, w := range want {
		if !slices.Contains(names, w) {
			t.Errorf("bundled skills missing %q (got %v)", w, names)
		}
	}
}

func TestWriteDefaults_emptyDir_writesAll(t *testing.T) {
	dir := t.TempDir()
	written, err := WriteDefaults(dir)
	if err != nil {
		t.Fatalf("WriteDefaults: %v", err)
	}
	if len(written) == 0 {
		t.Fatal("expected to write some defaults into empty dir")
	}
	// every written file is on disk and starts with the frontmatter fence
	for _, name := range written {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.HasPrefix(string(data), "---") {
			t.Errorf("%s lacks frontmatter fence", name)
		}
	}
}

func TestWriteDefaults_preservesUserEdits(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "calc.md")
	if err := os.WriteFile(target, []byte("user-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	written, err := WriteDefaults(dir)
	if err != nil {
		t.Fatalf("WriteDefaults: %v", err)
	}
	for _, n := range written {
		if n == "calc.md" {
			t.Fatal("WriteDefaults overwrote existing user file")
		}
	}
	data, _ := os.ReadFile(target)
	if string(data) != "user-content" {
		t.Errorf("user file modified: %q", data)
	}
}
