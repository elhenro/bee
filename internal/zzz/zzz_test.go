package zzz

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func init() {
	// route HomeDir into a t.TempDir-style fresh root for every test run via
	// HOME override. Tests that exercise meta/notes set HOME themselves.
	_ = context.Background
}

// newRepo builds a fresh git repo in t.TempDir with one initial commit.
// Skips the test if git isn't installed.
func newRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=zzz-test",
			"GIT_AUTHOR_EMAIL=zzz@test",
			"GIT_COMMITTER_NAME=zzz-test",
			"GIT_COMMITTER_EMAIL=zzz@test",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "commit.gpgsign", "false")
	// persist identity so subsequent Commit() calls outside this helper's
	// scoped env still produce valid commits (CI may have no global config).
	run("config", "user.name", "zzz-test")
	run("config", "user.email", "zzz@test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	return dir
}

func TestIsCleanAndDirty(t *testing.T) {
	repo := newRepo(t)
	clean, err := IsClean(repo)
	if err != nil || !clean {
		t.Fatalf("fresh repo should be clean: clean=%v err=%v", clean, err)
	}
	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	clean, _ = IsClean(repo)
	if clean {
		t.Fatal("untracked file should mark repo dirty")
	}
}

func TestCommitResetCycle(t *testing.T) {
	repo := newRepo(t)
	path := filepath.Join(repo, "new.txt")
	if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AddAll(repo); err != nil {
		t.Fatalf("AddAll: %v", err)
	}
	sha, err := Commit(repo, "feat: add new file\n\nbody", false, false)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if sha == "" {
		t.Fatal("empty sha after commit")
	}
	clean, _ := IsClean(repo)
	if !clean {
		t.Fatal("repo should be clean after commit")
	}
	// dirty it again, then ResetHard should roll it back.
	if err := os.WriteFile(path, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ResetHard(repo, ""); err != nil {
		t.Fatalf("ResetHard: %v", err)
	}
	clean, _ = IsClean(repo)
	if !clean {
		t.Fatal("repo should be clean after ResetHard")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "hi" {
		t.Fatalf("file not restored: got %q", b)
	}
}

func TestCleanFDRemovesUntracked(t *testing.T) {
	repo := newRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CleanFD(repo); err != nil {
		t.Fatalf("CleanFD: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "stray.txt")); !os.IsNotExist(err) {
		t.Fatalf("stray.txt should be gone; err=%v", err)
	}
}

func TestCreateBranchAndCurrent(t *testing.T) {
	repo := newRepo(t)
	if err := CreateBranchAndSwitch(repo, "zzz/test-1"); err != nil {
		t.Fatalf("create branch: %v", err)
	}
	cur, err := CurrentBranch(repo)
	if err != nil || cur != "zzz/test-1" {
		t.Fatalf("want zzz/test-1, got %q (err=%v)", cur, err)
	}
	if !HasBranch(repo, "zzz/test-1") {
		t.Fatal("HasBranch should report true for created branch")
	}
}

func TestCommitMessageFromShape(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"feat: add foo", "feat: add foo"},
		{"add a new file", "chore: add a new file"},
		{"# Header line\n\nbody here", "chore: Header line"},
		{"", "zzz: iter 5"}, // empty → fallback subject
	}
	for _, c := range cases {
		msg := CommitMessageFrom(c.in, 5, 10, 20)
		first := strings.SplitN(msg, "\n", 2)[0]
		want := c.want
		if c.in == "" {
			want = "chore: zzz: iter 5"
		}
		if first != want {
			t.Errorf("CommitMessageFrom(%q): subject=%q want %q", c.in, first, want)
		}
		if !strings.Contains(msg, "zzz-iter: 5") || !strings.Contains(msg, "zzz-tokens: 10 in / 20 out") {
			t.Errorf("missing footer in %q", msg)
		}
	}
}

func TestStateRoundTripAndNotes(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	id := NewID()
	r := &Run{
		ID:        id,
		Objective: "test obj",
		Branch:    "zzz/" + id,
		Mode:      ModeBranch,
		RepoRoot:  "/tmp/x",
		StartedAt: time.Now().UTC(),
		Status:    StatusRunning,
		Tokens:    TokenStat{Input: 10, Output: 5, USD: 0.0001},
	}
	if err := SaveMeta(r); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}
	if err := SavePrompt(id, "test obj"); err != nil {
		t.Fatalf("SavePrompt: %v", err)
	}
	loaded, err := LoadMeta(id)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if loaded.Objective != "test obj" || loaded.Tokens.Input != 10 {
		t.Errorf("round-trip lost data: %+v", loaded)
	}

	if err := AppendNote(id, 1, "added X", "diff summary"); err != nil {
		t.Fatalf("AppendNote: %v", err)
	}
	notes, err := ReadNotes(id)
	if err != nil {
		t.Fatalf("ReadNotes: %v", err)
	}
	if !strings.Contains(notes, "## iter 1 — added X") || !strings.Contains(notes, "diff summary") {
		t.Errorf("notes content wrong:\n%s", notes)
	}

	if err := AppendEvent(id, Event{Iter: 1, Phase: IterCommitted, Commit: "abc123"}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	saved, err := LatestRun()
	if err != nil || saved == nil || saved.ID != id {
		t.Errorf("LatestRun mismatch: %+v err=%v", saved, err)
	}

	// events.jsonl must actually exist under $HOME after AppendEvent.
	home, _ := HomeDir()
	jsonl, err := os.ReadFile(filepath.Join(home, "runs", id, "events.jsonl"))
	if err != nil {
		t.Fatalf("events.jsonl missing: %v", err)
	}
	if !strings.Contains(string(jsonl), `"phase":"committed"`) {
		t.Errorf("events.jsonl content wrong: %s", jsonl)
	}
}

func TestTailNoteSectionsKeepsLastN(t *testing.T) {
	notes := "\n## iter 1 — first\nbody1\n\n## iter 2 — second\nbody2\n\n## iter 3 — third\nbody3\n"
	if got := TailNoteSections(notes, 0); got != notes {
		t.Error("n=0 should be unchanged")
	}
	if got := TailNoteSections(notes, -1); got != notes {
		t.Error("n<0 should be unchanged")
	}
	got := TailNoteSections(notes, 1)
	if !strings.Contains(got, "iter 3 — third") || strings.Contains(got, "iter 1") {
		t.Errorf("n=1 should keep only the last section, got: %q", got)
	}
	got = TailNoteSections(notes, 2)
	if !strings.Contains(got, "iter 2") || !strings.Contains(got, "iter 3") || strings.Contains(got, "iter 1") {
		t.Errorf("n=2 should keep last two, got: %q", got)
	}
	if got := TailNoteSections(notes, 10); got != notes {
		t.Error("n>total should be unchanged")
	}
}

func TestCommitMessageFromUnicodeSafeTruncation(t *testing.T) {
	// 80 wide characters of a multi-byte rune. Byte slice would split mid-rune.
	long := strings.Repeat("ü", 80)
	msg := CommitMessageFrom(long, 1, 0, 0)
	subject := strings.SplitN(msg, "\n", 2)[0]
	if !strings.HasPrefix(subject, "chore: ") {
		t.Errorf("missing chore prefix: %q", subject)
	}
	rest := strings.TrimPrefix(subject, "chore: ")
	// must be valid UTF-8 (i.e. no replacement char or split rune).
	for _, r := range rest {
		if r == '�' {
			t.Fatalf("subject contains replacement char (mid-rune split): %q", rest)
		}
	}
}

func TestSaveBlockedPatch(t *testing.T) {
	repo := newRepo(t)
	t.Setenv("BEE_HOME", t.TempDir())
	id := NewID()
	// dirty the tree so the patch has content.
	if err := os.WriteFile(filepath.Join(repo, "scratch.txt"), []byte("partial work"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveBlockedPatch(id, 4, repo); err != nil {
		t.Fatalf("SaveBlockedPatch: %v", err)
	}
	home, _ := HomeDir()
	patch, err := os.ReadFile(filepath.Join(home, "runs", id, "blocked-4.patch"))
	if err != nil {
		t.Fatalf("patch file missing: %v", err)
	}
	if !strings.Contains(string(patch), "scratch.txt") {
		t.Errorf("patch should mention scratch.txt: %s", patch)
	}
}

func TestNewIDFormat(t *testing.T) {
	id := NewID()
	if len(id) != len("20060102")+1+8 {
		t.Errorf("unexpected NewID length: %q", id)
	}
	if !strings.Contains(id, "-") {
		t.Errorf("NewID should contain '-': %q", id)
	}
}

func TestStatusDisableSilent(t *testing.T) {
	s := NewStatus(io.Discard)
	s.Disable()
	s.SetIter(3, 10)
	s.SetTokens(TokenStat{Input: 100, Output: 50})
	s.Println("hi") // should not panic
}
