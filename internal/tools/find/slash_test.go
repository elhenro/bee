package find

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// a pattern with a slash and no '**' matches the root-relative path, not the
// basename, and is anchored at the root.
func TestFind_SlashPatternMatchesRelPath(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"pkg/a.go", "pkg/sub/b.go", "other/a.go", "a.go"} {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, _ := New(dir).Run(context.Background(), map[string]any{"name": "pkg/*.go"})
	if res.Content != "pkg/a.go" {
		t.Errorf("pkg/*.go: got %q", res.Content)
	}
	res, _ = New(dir).Run(context.Background(), map[string]any{"name": "**/sub/*.go"})
	if !strings.Contains(res.Content, "pkg/sub/b.go") || strings.Contains(res.Content, "other") {
		t.Errorf("**/sub/*.go: got %q", res.Content)
	}
}
