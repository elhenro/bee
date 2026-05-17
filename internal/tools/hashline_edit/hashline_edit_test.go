package hashline_edit

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/tools/apply_patch"
)

// writeFile dumps content to a fresh temp file and returns its path.
func writeFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// ref builds a "<n>#<tag>" anchor for the i-th 1-based line of content.
func ref(content string, lineNum int) string {
	tags := apply_patch.TagAll(strings.TrimSuffix(content, "\n"))
	return apply_patch.Ref(strings.Split(strings.TrimSuffix(content, "\n"), "\n")[lineNum-1], lineNum) + "_" + tags[lineNum-1]
}

// anchor returns "<n>#<tag>" for the given live content's lineNum.
func anchor(content string, lineNum int) string {
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	return apply_patch.Ref(lines[lineNum-1], lineNum)
}

func runTool(t *testing.T, in map[string]any) (string, bool) {
	t.Helper()
	res, err := New().Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	return res.Content, res.IsError
}

func TestReplaceValid(t *testing.T) {
	content := "alpha\nbeta\ngamma\n"
	path := writeFile(t, content)

	pos := anchor(content, 2)
	_, isErr := runTool(t, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{"pos": pos, "op": "replace", "lines": []any{"BETA-NEW"}},
		},
	})
	if isErr {
		t.Fatalf("expected success, got error")
	}
	got, _ := os.ReadFile(path)
	want := "alpha\nBETA-NEW\ngamma\n"
	if string(got) != want {
		t.Fatalf("content mismatch:\n got: %q\nwant: %q", string(got), want)
	}
}

func TestReplaceHashMismatch(t *testing.T) {
	content := "alpha\nbeta\ngamma\n"
	path := writeFile(t, content)

	_, isErr := runTool(t, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{"pos": "2#AA", "op": "replace", "lines": []any{"NOPE"}},
		},
	})
	if !isErr {
		t.Fatalf("expected error on hash mismatch, got success")
	}
	got, _ := os.ReadFile(path)
	if string(got) != content {
		t.Fatalf("file was modified on rejection; got %q", string(got))
	}
}

func TestMultipleEditsValid(t *testing.T) {
	content := "one\ntwo\nthree\nfour\nfive\n"
	path := writeFile(t, content)

	p1 := anchor(content, 1)
	p3 := anchor(content, 3)
	p5 := anchor(content, 5)

	_, isErr := runTool(t, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{"pos": p1, "op": "replace", "lines": []any{"ONE"}},
			map[string]any{"pos": p3, "op": "replace", "lines": []any{"THREE"}},
			map[string]any{"pos": p5, "op": "replace", "lines": []any{"FIVE"}},
		},
	})
	if isErr {
		t.Fatalf("expected success, got error")
	}
	got, _ := os.ReadFile(path)
	want := "ONE\ntwo\nTHREE\nfour\nFIVE\n"
	if string(got) != want {
		t.Fatalf("content mismatch:\n got: %q\nwant: %q", string(got), want)
	}
}

// Edits at line 5 (replace with 3 lines) and line 2 (replace with 1 line)
// must both target the ORIGINAL snapshot. After bottom-up application both
// land at the expected places without shifting each other.
func TestBottomUpApplication(t *testing.T) {
	content := "L1\nL2\nL3\nL4\nL5\nL6\n"
	path := writeFile(t, content)

	p2 := anchor(content, 2)
	p5 := anchor(content, 5)

	_, isErr := runTool(t, map[string]any{
		"path": path,
		"edits": []any{
			// passed in low-to-high; tool must sort and apply high-first.
			map[string]any{"pos": p2, "op": "replace", "lines": []any{"TWO"}},
			map[string]any{"pos": p5, "op": "replace", "lines": []any{"FIVE-A", "FIVE-B", "FIVE-C"}},
		},
	})
	if isErr {
		t.Fatalf("expected success, got error")
	}
	got, _ := os.ReadFile(path)
	want := "L1\nTWO\nL3\nL4\nFIVE-A\nFIVE-B\nFIVE-C\nL6\n"
	if string(got) != want {
		t.Fatalf("content mismatch:\n got: %q\nwant: %q", string(got), want)
	}
}

func TestPrependVsAppend(t *testing.T) {
	content := "a\nb\nc\n"
	pathPrep := writeFile(t, content)
	pathApp := writeFile(t, content)

	p2 := anchor(content, 2)

	// prepend at line 2 → insertion goes BEFORE "b" (index 1).
	if _, isErr := runTool(t, map[string]any{
		"path": pathPrep,
		"edits": []any{
			map[string]any{"pos": p2, "op": "prepend", "lines": []any{"X"}},
		},
	}); isErr {
		t.Fatalf("prepend failed")
	}
	gotP, _ := os.ReadFile(pathPrep)
	wantP := "a\nX\nb\nc\n"
	if string(gotP) != wantP {
		t.Fatalf("prepend mismatch:\n got: %q\nwant: %q", string(gotP), wantP)
	}

	// append at line 2 → insertion goes AFTER "b" (index 2).
	if _, isErr := runTool(t, map[string]any{
		"path": pathApp,
		"edits": []any{
			map[string]any{"pos": p2, "op": "append", "lines": []any{"X"}},
		},
	}); isErr {
		t.Fatalf("append failed")
	}
	gotA, _ := os.ReadFile(pathApp)
	wantA := "a\nb\nX\nc\n"
	if string(gotA) != wantA {
		t.Fatalf("append mismatch:\n got: %q\nwant: %q", string(gotA), wantA)
	}
}

func TestOverlappingEditsRejected(t *testing.T) {
	content := "alpha\nbeta\ngamma\n"
	path := writeFile(t, content)
	p2 := anchor(content, 2)

	_, isErr := runTool(t, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{"pos": p2, "op": "replace", "lines": []any{"A"}},
			map[string]any{"pos": p2, "op": "replace", "lines": []any{"B"}},
		},
	})
	if !isErr {
		t.Fatalf("expected overlap rejection, got success")
	}
	got, _ := os.ReadFile(path)
	if string(got) != content {
		t.Fatalf("file was modified on overlap rejection; got %q", string(got))
	}
}

func TestLineOutOfRange(t *testing.T) {
	content := "alpha\nbeta\n"
	path := writeFile(t, content)

	_, isErr := runTool(t, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{"pos": "99#" + apply_patch.Tag("alpha", 99), "op": "replace", "lines": []any{"NOPE"}},
		},
	})
	if !isErr {
		t.Fatalf("expected out-of-range error, got success")
	}
}

func TestBadPos(t *testing.T) {
	path := writeFile(t, "alpha\n")
	for _, bad := range []string{"", "abc", "1#", "1#A", "1#ABC", "#AA", "0#AA", "-1#AA"} {
		_, isErr := runTool(t, map[string]any{
			"path": path,
			"edits": []any{
				map[string]any{"pos": bad, "op": "replace", "lines": []any{"x"}},
			},
		})
		if !isErr {
			t.Fatalf("expected bad-pos error for %q", bad)
		}
	}
}

func TestUnknownOp(t *testing.T) {
	content := "alpha\nbeta\n"
	path := writeFile(t, content)
	p1 := anchor(content, 1)

	_, isErr := runTool(t, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{"pos": p1, "op": "frobnicate", "lines": []any{"x"}},
		},
	})
	if !isErr {
		t.Fatalf("expected unknown-op error, got success")
	}
}

// Silence the unused helper warning if it ever stops being used.
var _ = ref

func TestHashlineEdit_FilterNilAllowsAll(t *testing.T) {
	content := "alpha\nbeta\n"
	path := writeFile(t, content)
	pos := anchor(content, 1)
	res, err := NewWithFilter(nil).Run(context.Background(), map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{"pos": pos, "op": "replace", "lines": []any{"ALPHA"}},
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("nil filter must allow: %v %s", err, res.Content)
	}
}

func TestHashlineEdit_FilterAllowsMatch(t *testing.T) {
	content := "alpha\nbeta\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "f.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	pos := anchor(content, 1)
	tool := NewWithFilter(regexp.MustCompile(`\.md$`))
	res, _ := tool.Run(context.Background(), map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{"pos": pos, "op": "replace", "lines": []any{"ALPHA"}},
		},
	})
	if res.IsError {
		t.Fatalf("md path must pass: %s", res.Content)
	}
	got, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(got), "ALPHA") {
		t.Errorf("expected edit applied, got %q", got)
	}
}

func TestHashlineEdit_FilterRejectsMiss(t *testing.T) {
	content := "alpha\nbeta\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	pos := anchor(content, 1)
	tool := NewWithFilter(regexp.MustCompile(`\.md$`))
	res, _ := tool.Run(context.Background(), map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{"pos": pos, "op": "replace", "lines": []any{"ALPHA"}},
		},
	})
	if !res.IsError {
		t.Fatalf("want IsError for non-md path")
	}
	if !strings.Contains(res.Content, "denied") {
		t.Errorf("want 'denied' in msg, got: %s", res.Content)
	}
	got, _ := os.ReadFile(path)
	if string(got) != content {
		t.Errorf("file must be untouched on rejection, got %q", got)
	}
}

// echo: after a successful edit, the result should contain fresh anchors
// for the new range + 2 lines of context. Saves a re-read round-trip.
func TestResultEchoesFreshAnchors(t *testing.T) {
	content := "one\ntwo\nthree\nfour\nfive\n"
	path := writeFile(t, content)
	pos := anchor(content, 3)
	out, isErr := runTool(t, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{"pos": pos, "op": "replace", "lines": []any{"THREE-NEW"}},
		},
	})
	if isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	// header line is the verb + count
	if !strings.HasPrefix(out, "applied 1 edit(s) to ") {
		t.Fatalf("missing header: %s", out)
	}
	// echoed body must contain the new content with its anchor + the new
	// separator " │ " (not a tab)
	if !strings.Contains(out, " │ THREE-NEW") {
		t.Fatalf("echo missing new content with pipe separator:\n%s", out)
	}
	// every echoed line should match "<lineN>#<2-char> │ " — verify at least
	// one fully-formed anchor appears.
	if !strings.Contains(out, "#") || !strings.Contains(out, " │ ") {
		t.Fatalf("echo missing #TAG │ format:\n%s", out)
	}
}

// dry_run: validate + echo what would change, but the file is NOT written.
func TestDryRunDoesNotWrite(t *testing.T) {
	content := "alpha\nbeta\ngamma\n"
	path := writeFile(t, content)
	pos := anchor(content, 2)
	out, isErr := runTool(t, map[string]any{
		"path":    path,
		"dry_run": true,
		"edits": []any{
			map[string]any{"pos": pos, "op": "replace", "lines": []any{"BETA-NEW"}},
		},
	})
	if isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	if !strings.HasPrefix(out, "dry-run 1 edit(s) to ") {
		t.Fatalf("dry-run verb missing: %s", out)
	}
	// file MUST be untouched
	got, _ := os.ReadFile(path)
	if string(got) != content {
		t.Fatalf("dry_run wrote to disk; got %q want %q", string(got), content)
	}
	// echo still shows the would-be content
	if !strings.Contains(out, "BETA-NEW") {
		t.Fatalf("dry-run echo missing new content:\n%s", out)
	}
}

// multi-edit echo positions: when two edits at different lines land in the
// same file, the post-shift line numbers in the echo must reflect the
// cumulative shift from edits above each one.
func TestMultiEditEchoShiftCorrect(t *testing.T) {
	content := "one\ntwo\nthree\nfour\nfive\n"
	path := writeFile(t, content)
	// prepend 2 lines at line 2, then replace line 4 (originally "four").
	// after prepend: file is "one\nA\nB\ntwo\nthree\nfour\nfive\n"
	// after replace of orig-line-4 ("four", now at line 6): line 6 → "FOUR"
	p2 := anchor(content, 2)
	p4 := anchor(content, 4)
	out, isErr := runTool(t, map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{"pos": p2, "op": "prepend", "lines": []any{"A", "B"}},
			map[string]any{"pos": p4, "op": "replace", "lines": []any{"FOUR"}},
		},
	})
	if isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	want := "one\nA\nB\ntwo\nthree\nFOUR\nfive\n"
	got, _ := os.ReadFile(path)
	if string(got) != want {
		t.Fatalf("content wrong:\n got %q\nwant %q", string(got), want)
	}
	// echo must report the prepend's new range as lines 2-3 and the
	// replace's new range as line 6 (because of the 2-line shift).
	if !strings.Contains(out, "prepend @ orig line 2 → new lines 2-3") {
		t.Fatalf("prepend echo wrong:\n%s", out)
	}
	if !strings.Contains(out, "replace @ orig line 4 → new lines 6-6") {
		t.Fatalf("replace echo wrong (shift not applied):\n%s", out)
	}
}
