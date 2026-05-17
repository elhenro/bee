package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspace_HiddenByDefault(t *testing.T) {
	w := NewWorkspace()
	if w.Visible() {
		t.Fatal("workspace should start hidden")
	}
	if got := w.View(60, 12); got != "" {
		t.Fatalf("hidden view should be empty, got %q", got)
	}
}

func TestWorkspace_ToggleMsg(t *testing.T) {
	w := NewWorkspace()
	w.Update(ToggleWorkspaceMsg{})
	if !w.Visible() {
		t.Fatal("toggle should show")
	}
	w.Update(ToggleWorkspaceMsg{})
	if w.Visible() {
		t.Fatal("toggle should hide")
	}
}

func TestWorkspace_EmptyShowsHint(t *testing.T) {
	w := NewWorkspace()
	w.SetVisible(true)
	out := strip(w.View(60, 12))
	if !strings.Contains(out, "no file selected") {
		t.Fatalf("expected hint, got %q", out)
	}
}

func TestWorkspace_SetFileShowsContent(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "demo.go")
	if err := os.WriteFile(path, []byte("package demo\n\nfunc Hello() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := NewWorkspace()
	w.SetVisible(true)
	if err := w.SetFile(path); err != nil {
		t.Fatal(err)
	}
	out := strip(w.View(60, 12))
	if !strings.Contains(out, "Hello") {
		t.Fatalf("expected file body, got %q", out)
	}
	if !strings.Contains(out, "demo.go") {
		t.Fatalf("expected header label, got %q", out)
	}
	if !strings.Contains(out, "· go") {
		t.Fatalf("expected type marker, got %q", out)
	}
}

func TestWorkspace_SetDiffRendersHunk(t *testing.T) {
	diff := `--- a/foo.txt
+++ b/foo.txt
@@ -1,3 +1,3 @@
 first
-second old
+second new
 third
`
	w := NewWorkspace()
	w.SetVisible(true)
	if err := w.SetDiff(diff); err != nil {
		t.Fatalf("SetDiff: %v", err)
	}
	out := strip(w.View(60, 16))
	if !strings.Contains(out, "+ second new") {
		t.Fatalf("expected added line, got %q", out)
	}
	if !strings.Contains(out, "- second old") {
		t.Fatalf("expected deleted line, got %q", out)
	}
}

func TestWorkspace_SetFileMissing(t *testing.T) {
	w := NewWorkspace()
	if err := w.SetFile("/does/not/exist"); err == nil {
		t.Fatal("want error for missing file")
	}
}

func TestWorkspace_ClearDiff(t *testing.T) {
	w := NewWorkspace()
	if err := w.SetDiff(""); err != nil {
		t.Fatalf("empty SetDiff returned err: %v", err)
	}
	w.ClearDiff()
	if w.diffFile != nil {
		t.Fatal("ClearDiff did not drop file")
	}
}
