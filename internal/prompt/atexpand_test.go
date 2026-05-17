package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandAtPaths_basic(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi there\n"), 0o644))

	in := "explain @hello.txt please"
	out := ExpandAtPaths(in, dir)

	if !strings.Contains(out, "### @hello.txt") {
		t.Fatalf("missing header: %q", out)
	}
	if !strings.Contains(out, "hi there") {
		t.Fatalf("missing body: %q", out)
	}
	if !strings.HasPrefix(out, "explain ") {
		t.Fatalf("lost prefix: %q", out)
	}
	if !strings.HasSuffix(out, " please") {
		t.Fatalf("lost suffix: %q", out)
	}
}

func TestExpandAtPaths_emailNotMatched(t *testing.T) {
	dir := t.TempDir()
	in := "ping user@example.com about this"
	out := ExpandAtPaths(in, dir)
	if out != in {
		t.Fatalf("email expanded; got %q", out)
	}
}

func TestExpandAtPaths_missingLeftAlone(t *testing.T) {
	dir := t.TempDir()
	in := "look at @does/not/exist.go"
	out := ExpandAtPaths(in, dir)
	if out != in {
		t.Fatalf("missing path mutated: %q", out)
	}
}

func TestExpandAtPaths_binarySkipped(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "bin.dat"), []byte{0, 1, 2, 3, 4}, 0o644))
	in := "read @bin.dat"
	out := ExpandAtPaths(in, dir)
	if out != in {
		t.Fatalf("binary expanded: %q", out)
	}
}

func TestExpandAtPaths_escapeAttemptBlocked(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Dir(dir)
	must(t, os.WriteFile(filepath.Join(parent, "outside.txt"), []byte("nope"), 0o644))
	t.Cleanup(func() { _ = os.Remove(filepath.Join(parent, "outside.txt")) })

	in := "exfil @../outside.txt"
	out := ExpandAtPaths(in, dir)
	if strings.Contains(out, "nope") {
		t.Fatalf("escape succeeded: %q", out)
	}
}

func TestExpandAtPaths_truncation(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("a", MaxFileBytes+1000)
	must(t, os.WriteFile(filepath.Join(dir, "big.txt"), []byte(big), 0o644))

	out := ExpandAtPaths("see @big.txt", dir)
	if !strings.Contains(out, "(truncated at") {
		t.Fatalf("expected truncation note: %q", out[:200])
	}
}

func TestExpandAtPaths_multipleTokens(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("AAA"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("BBB"), 0o644))

	out := ExpandAtPaths("diff @a.txt vs @b.txt", dir)
	if !strings.Contains(out, "AAA") || !strings.Contains(out, "BBB") {
		t.Fatalf("missing one file: %q", out)
	}
}

func TestExpandAtPaths_noAtNoop(t *testing.T) {
	if got := ExpandAtPaths("plain text", "/tmp"); got != "plain text" {
		t.Fatalf("noop violated: %q", got)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
