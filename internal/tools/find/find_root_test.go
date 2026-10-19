package find

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobInsideSkippedDirName(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"pkg/testdata/a.txt", "pkg/vendor/b.txt", "pkg/keep.txt"} {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(sub string) string {
		res, _ := New("").Run(context.Background(), map[string]any{"pattern": "*.txt", "path": filepath.Join(root, sub)})
		return res.Content
	}
	if got := run("pkg/testdata"); !strings.Contains(got, "a.txt") {
		t.Errorf("explicit testdata root: got %q", got)
	}
	if got := run("pkg/vendor"); !strings.Contains(got, "b.txt") {
		t.Errorf("explicit vendor root: got %q", got)
	}
	got := run("pkg")
	if !strings.Contains(got, "keep.txt") || strings.Contains(got, "a.txt") || strings.Contains(got, "b.txt") {
		t.Errorf("pkg walk should skip testdata/vendor children, got %q", got)
	}
}
