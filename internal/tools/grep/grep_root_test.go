package grep

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchInsideSkippedDirName(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"pkg/testdata/a.txt", "pkg/vendor/b.txt", "node_modules/c.txt", "pkg/keep.txt"} {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("needle\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(sub string) string {
		res, _ := New("").Run(context.Background(), map[string]any{"pattern": "needle", "path": filepath.Join(root, sub)})
		return res.Content
	}
	for sub, want := range map[string]string{"pkg/testdata": "a.txt", "pkg/vendor": "b.txt", "node_modules": "c.txt"} {
		if got := run(sub); !strings.Contains(got, want) {
			t.Errorf("explicit root %s: got %q, want a match in %s", sub, got, want)
		}
	}
	// below the root the skip list still applies
	got := run("pkg")
	if !strings.Contains(got, "keep.txt") || strings.Contains(got, "a.txt") || strings.Contains(got, "b.txt") {
		t.Errorf("pkg walk should skip testdata/vendor children, got %q", got)
	}
}
