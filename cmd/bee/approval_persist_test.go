package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AllowAlways persistence must not strip the user's comments or reorder their
// config — the insert is textual, validated by re-parse.
func TestPersistAllowlistEntry_PreservesComments(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	t.Setenv("BEE_CONFIG", p)

	orig := `# my bee config — do not lose this comment
default_provider = "ollama" # inline comment

[sandbox]
# allowlist below
command_allowlist = ["git status"]
`
	if err := os.WriteFile(p, []byte(orig), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := PersistAllowlistEntry("go test"); err != nil {
		t.Fatalf("persist: %v", err)
	}

	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	s := string(got)
	for _, want := range []string{
		"# my bee config — do not lose this comment",
		"# inline comment",
		"# allowlist below",
		`"go test"`,
		`"git status"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in rewritten config:\n%s", want, s)
		}
	}

	// dedup: persisting again must be a no-op
	if err := PersistAllowlistEntry("go test"); err != nil {
		t.Fatalf("persist dup: %v", err)
	}
	got2, _ := os.ReadFile(p)
	if string(got2) != s {
		t.Fatalf("duplicate persist rewrote file:\n%s", string(got2))
	}
}

func TestPersistAllowlistEntry_CreatesMissingPieces(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	t.Setenv("BEE_CONFIG", p)

	// no file at all
	if err := PersistAllowlistEntry("ls"); err != nil {
		t.Fatalf("persist into missing file: %v", err)
	}
	if !allowlistContainsFile(t, p, "ls") {
		t.Fatal("entry missing after create")
	}

	// section exists, key missing
	if err := os.WriteFile(p, []byte("# keep\n[sandbox]\nscope = \"workspace-write\"\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := PersistAllowlistEntry("pwd"); err != nil {
		t.Fatalf("persist into section: %v", err)
	}
	s, _ := os.ReadFile(p)
	if !strings.Contains(string(s), "# keep") || !allowlistContainsFile(t, p, "pwd") {
		t.Fatalf("section insert broken:\n%s", string(s))
	}

	// multi-line array
	ml := "[sandbox]\ncommand_allowlist = [\n  \"git status\", # note\n]\n"
	if err := os.WriteFile(p, []byte(ml), 0o644); err != nil {
		t.Fatalf("seed ml: %v", err)
	}
	if err := PersistAllowlistEntry("cat"); err != nil {
		t.Fatalf("persist multiline: %v", err)
	}
	s, _ = os.ReadFile(p)
	if !strings.Contains(string(s), "# note") || !allowlistContainsFile(t, p, "cat") || !allowlistContainsFile(t, p, "git status") {
		t.Fatalf("multiline insert broken:\n%s", string(s))
	}
}

func allowlistContainsFile(t *testing.T, path, key string) bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return allowlistContains(string(b), key)
}
