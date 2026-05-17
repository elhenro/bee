package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExpandImports_Basic(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "notes.md"), "NOTES_BODY")
	got := expandContextImports("see @notes.md for context", dir, map[string]bool{}, 0)
	if !strings.Contains(got, "NOTES_BODY") {
		t.Fatalf("import not inlined: %q", got)
	}
	if !strings.Contains(got, "<import path=") {
		t.Fatalf("missing import wrapper: %q", got)
	}
}

func TestExpandImports_Recursive(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "A then @b.md")
	writeFile(t, filepath.Join(dir, "b.md"), "B then @c.md")
	writeFile(t, filepath.Join(dir, "c.md"), "C_LEAF")
	got := expandContextImports("@a.md", dir, map[string]bool{}, 0)
	if !strings.Contains(got, "C_LEAF") {
		t.Fatalf("recursive expansion stopped early: %q", got)
	}
}

func TestExpandImports_Cycle(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "A @b.md")
	writeFile(t, filepath.Join(dir, "b.md"), "B @a.md")
	got := expandContextImports("@a.md", dir, map[string]bool{}, 0)
	// must terminate; second a.md ref should not re-expand
	if strings.Count(got, "<import") > 2 {
		t.Fatalf("cycle re-expanded: %q", got)
	}
}

func TestExpandImports_DepthCap(t *testing.T) {
	dir := t.TempDir()
	// chain longer than maxImportDepth (5)
	for i := 0; i < 7; i++ {
		name := filepath.Join(dir, "f"+string(rune('0'+i))+".md")
		body := "L" + string(rune('0'+i))
		if i < 6 {
			body += " @f" + string(rune('0'+i+1)) + ".md"
		}
		writeFile(t, name, body)
	}
	got := expandContextImports("@f0.md", dir, map[string]bool{}, 0)
	// depth cap = 5 → at most 5 nested wrappers
	if strings.Count(got, "<import") > maxImportDepth {
		t.Fatalf("depth cap not enforced: %d wrappers in %q",
			strings.Count(got, "<import"), got)
	}
}

func TestExpandImports_FuzzyFallback(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "docs", "PLAN.md"), "PLAN_BODY")
	// lowercase + partial — should fzf-match PLAN.md
	got := expandContextImports("see @plan.md", dir, map[string]bool{}, 0)
	if !strings.Contains(got, "PLAN_BODY") {
		t.Fatalf("fuzzy fallback failed: %q", got)
	}
}

func TestExpandImports_MissingFileLeftAsIs(t *testing.T) {
	dir := t.TempDir()
	in := "no such file @ghost-xyz-no-match.md here"
	got := expandContextImports(in, dir, map[string]bool{}, 0)
	if got != in {
		t.Fatalf("missing-file token mutated: %q", got)
	}
}

func TestExpandImports_NoAtPrefixSkipped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "x.md"), "X")
	// email-like — must NOT expand
	in := "user@x.md is an email"
	got := expandContextImports(in, dir, map[string]bool{}, 0)
	if strings.Contains(got, "<import") {
		t.Fatalf("email-like @ incorrectly expanded: %q", got)
	}
}

func TestExpandImports_HomeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	tmp, err := os.CreateTemp(home, "bee-import-test-*.md")
	if err != nil {
		t.Skip("cannot write to home")
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString("HOME_BODY"); err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	ref := "@~/" + filepath.Base(tmp.Name())
	got := expandContextImports(ref, t.TempDir(), map[string]bool{}, 0)
	if !strings.Contains(got, "HOME_BODY") {
		t.Fatalf("~ expansion failed: %q", got)
	}
}

func TestExpandImports_BinarySkipped(t *testing.T) {
	dir := t.TempDir()
	bin := append([]byte("hi"), 0, 0, 0)
	if err := os.WriteFile(filepath.Join(dir, "bin.md"), bin, 0o644); err != nil {
		t.Fatal(err)
	}
	got := expandContextImports("@bin.md", dir, map[string]bool{}, 0)
	if strings.Contains(got, "<import") {
		t.Fatalf("binary file was expanded: %q", got)
	}
}

func TestLoadContextFiles_ExpandsImports(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "AGENTS.md"), "main @sub.md tail")
	writeFile(t, filepath.Join(dir, "sub.md"), "SUB_BODY")
	got := LoadContextFiles(dir, "")
	if len(got) != 1 {
		t.Fatalf("want 1 file, got %d", len(got))
	}
	if !strings.Contains(got[0].Body, "SUB_BODY") {
		t.Fatalf("import not expanded in tryLoad: %q", got[0].Body)
	}
}
