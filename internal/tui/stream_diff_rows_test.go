package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/types"
)

const twoHunkPatch = "diff --git a/s.go b/s.go\n--- a/s.go\n+++ b/s.go\n" +
	"@@ -1,5 +1,6 @@\n a\n b\n+ins\n c\n d\n e\n" +
	"@@ -20,4 +21,4 @@\n x\n-old\n+new\n y\n z\n"

func TestParsePatch_NumbersBothSidesAcrossHunks(t *testing.T) {
	files := parsePatch(twoHunkPatch)
	if len(files) != 1 {
		t.Fatalf("want 1 file, got %d", len(files))
	}
	f := files[0]
	if f.path != "s.go" || f.adds != 2 || f.dels != 1 {
		t.Fatalf("got path=%q adds=%d dels=%d", f.path, f.adds, f.dels)
	}
	var ins, old, nw editOp
	for _, op := range f.ops {
		switch op.text {
		case "ins":
			ins = op
		case "old":
			old = op
		case "new":
			nw = op
		}
	}
	if ins.new != 3 || ins.old != 3 {
		t.Fatalf("ins counters = old %d new %d, want 3/3", ins.old, ins.new)
	}
	if old.old != 21 || nw.new != 22 {
		t.Fatalf("second hunk: old row at %d, new row at %d, want 21/22", old.old, nw.new)
	}
}

func TestParsePatch_NewAndDeletedFiles(t *testing.T) {
	files := parsePatch("--- /dev/null\n+++ b/n.go\n@@ -0,0 +1,2 @@\n+a\n+b\n" +
		"diff --git a/o.go b/o.go\n--- a/o.go\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-a\n-b\n")
	if len(files) != 2 {
		t.Fatalf("want 2 files, got %d: %+v", len(files), files)
	}
	if files[0].path != "n.go" || files[0].state != "new" || files[0].adds != 2 || files[0].ops[1].new != 2 {
		t.Fatalf("new file parsed wrong: %+v", files[0])
	}
	if files[1].path != "o.go" || files[1].state != "deleted" || files[1].dels != 2 || files[1].ops[1].old != 2 {
		t.Fatalf("deleted file parsed wrong: %+v", files[1])
	}
}

// a removed line reading `-- note` shows up as `--- note`: only the hunk counts
// tell it apart from a file header
func TestParsePatch_RemovedDashLineIsNotAFileHeader(t *testing.T) {
	files := parsePatch("--- a/q.sql\n+++ b/q.sql\n@@ -1,2 +1,1 @@\n--- note\n keep\n")
	if len(files) != 1 {
		t.Fatalf("want 1 file, got %d: %+v", len(files), files)
	}
	if got := files[0].ops[0]; got.kind != '-' || got.text != "-- note" {
		t.Fatalf("first op = %+v, want removal of %q", got, "-- note")
	}
}

func TestParsePatch_BareHeadersSplitFiles(t *testing.T) {
	files := parsePatch("--- a/x\n+++ b/x\n@@ -1 +1 @@\n-1\n+2\n--- a/y\n+++ b/y\n@@ -1 +1 @@\n-3\n+4\n")
	if len(files) != 2 || files[0].path != "x" || files[1].path != "y" {
		t.Fatalf("got %+v", files)
	}
}

func TestParsePatch_TolerantOfBlankContextAndNoNewlineMarker(t *testing.T) {
	files := parsePatch("--- a/x\n+++ b/x\n@@ -1,3 +1,3 @@\n a\n\n-c\n\\ No newline at end of file\n+d\n")
	if len(files) != 1 || len(files[0].ops) != 4 {
		t.Fatalf("got %+v", files)
	}
	if blank := files[0].ops[1]; blank.kind != '=' || blank.text != "" {
		t.Fatalf("blank context row = %+v", blank)
	}
}

// the gap between two shown rows is read off the line numbers, so it counts
// the lines trimmed from the context as well as the ones between the hunks
func TestLayoutRows_GapComesFromLineNumbers(t *testing.T) {
	ops := parsePatch(twoHunkPatch)[0].ops
	rows := layoutRows(collapseToHunks(ops, diffContextLines))
	var gaps []string
	for _, rw := range rows {
		if rw.kind == 'm' {
			gaps = append(gaps, rw.text)
		}
	}
	// context trims to one row: `c` (old line 3) is the last row shown before
	// `x` (old line 20), so lines 4-19 are skipped, trimmed and unlisted alike
	want := glyphGap + " +16 unchanged"
	if len(gaps) != 1 || gaps[0] != want {
		t.Fatalf("gaps = %q, want [%q]", gaps, want)
	}
	if rows[0].no == 0 {
		t.Fatalf("numbered ops lost their numbers: %+v", rows[0])
	}
}

func TestLayoutRows_UnnumberedKeepsCollapserMarkers(t *testing.T) {
	var a, b []string
	for i := 0; i < 20; i++ {
		a = append(a, fmt.Sprint("line", i))
		b = append(b, fmt.Sprint("line", i))
	}
	b[10] = "CHANGED"
	rows := layoutRows(collapseToHunks(lineDiff(a, b), 1))
	var marks int
	for _, rw := range rows {
		if rw.kind == 'm' && strings.Contains(rw.text, "unchanged") {
			marks++
		}
		if rw.no != 0 {
			t.Fatalf("unnumbered diff grew a line number: %+v", rw)
		}
	}
	if marks != 2 {
		t.Fatalf("want above+below markers, got %d in %+v", marks, rows)
	}
}

func TestEditAnchor(t *testing.T) {
	use := types.ToolUse{Name: "edit"}
	echo := types.ToolResult{Content: "replaced 1 occurrence(s) in a.go\n\n# region 1 → lines 17-19\n    17#AB │ x"}
	if got := editAnchor(use, echo); got != 17 {
		t.Fatalf("anchor = %d, want 17", got)
	}
	if got := editAnchor(use, types.ToolResult{IsError: true, Content: echo.Content}); got != 0 {
		t.Fatalf("failed edit anchored at %d", got)
	}
	if got := editAnchor(types.ToolUse{Name: "write"}, echo); got != 0 {
		t.Fatalf("non-edit anchored at %d", got)
	}
	if got := editAnchor(use, types.ToolResult{Content: "replaced 1 occurrence(s)"}); got != 0 {
		t.Fatalf("echo without a region anchored at %d", got)
	}
}

func TestPairedEdit_NumbersRowsFromTheEcho(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	use := types.ToolUse{ID: "e1", Name: "edit", Input: map[string]any{
		"path": "a.go", "old": "x := 1\n", "new": "x := 2\ny := 3\n",
	}}
	r.toolUses = map[string]types.ToolUse{"e1": use}
	res := types.ToolResult{UseID: "e1", Content: "replaced 1 occurrence(s) in a.go\n\n# region 1 → lines 17-18"}
	out := stripANSI(r.pairedToolResult(res))
	for _, want := range []string{"17 - x := 1", "17 + x := 2", "18 + y := 3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestTrimRows_CutMarkerSitsWhereTheCutIs(t *testing.T) {
	var rows []diffRow
	for i := 0; i < 40; i++ {
		rows = append(rows, diffRow{kind: '-', text: "o"})
	}
	for i := 0; i < 40; i++ {
		rows = append(rows, diffRow{kind: '+', text: "n"})
	}
	got := trimRows(rows, 14)
	if len(got) != 15 || got[7].kind != 'm' || got[7].text != glyphMore+" +66 lines" {
		t.Fatalf("removal-heavy cut wrong: %+v", got)
	}
	if got[0].kind != '-' || got[14].kind != '+' {
		t.Fatalf("both sides must stay represented: %+v", got)
	}

	adds := rows[40:]
	got = trimRows(adds, 14)
	if len(got) != 15 || got[14].kind != 'm' || got[14].text != glyphMore+" +26 lines" {
		t.Fatalf("addition-only cut wrong: %+v", got)
	}
	if short := trimRows(adds[:5], 14); len(short) != 5 {
		t.Fatalf("short diff was trimmed: %+v", short)
	}
}

func TestPreviewWrite_NoPhantomRowAfterTrailingNewline(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	out := stripANSI(r.renderToolUse(types.ToolUse{Name: "write", Input: map[string]any{
		"path": "a.txt", "content": "one\ntwo\n",
	}}))
	if !strings.Contains(out, "+2") || strings.Contains(out, "3 +") {
		t.Fatalf("trailing newline counted as a line:\n%s", out)
	}
}

func TestApplyPatch_MultiFileCardLabelsEachFile(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 90)
	patch := "--- /dev/null\n+++ b/new.go\n@@ -0,0 +1,1 @@\n+package x\n" +
		"--- a/old.go\n+++ b/old.go\n@@ -3,2 +3,2 @@\n a\n-b\n+c\n"
	out := stripANSI(r.renderToolUse(types.ToolUse{Name: "apply_patch", Input: map[string]any{"patch": patch}}))
	for _, want := range []string{"2 files", "+2", "−1", "new.go (new)", "old.go", "1 + package x", "4 - b", "4 + c"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestDiffCard_RowsNeverExceedWidth(t *testing.T) {
	long := strings.Repeat("word ", 40)
	patch := "--- a/f.go\n+++ b/f.go\n@@ -98,2 +98,2 @@\n ctx " + long + "\n-" + long + "\n+" + long + "x\n"
	for _, w := range []int{40, 60, 100} {
		r := NewStreamRenderer(DefaultStyles(), w)
		out := r.renderToolUse(types.ToolUse{Name: "apply_patch", Input: map[string]any{"patch": patch}})
		for _, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			if got := lipgloss.Width(ln); got > w {
				t.Fatalf("width %d: row is %d cells: %q", w, got, stripANSI(ln))
			}
		}
	}
}

func TestSummarizeSearch_DefaultScopeStaysQuiet(t *testing.T) {
	if got := summarizeToolArgs("search", map[string]any{"pattern": "Timeout", "path": "."}, 80); got != "Timeout" {
		t.Fatalf("root scope leaked into the row: %q", got)
	}
	if got := summarizeToolArgs("search", map[string]any{"pattern": "Timeout", "path": "internal"}, 80); !strings.Contains(got, "· internal") {
		t.Fatalf("narrowed scope missing: %q", got)
	}
	if got := summarizeToolArgs("glob", map[string]any{"name": "*.go", "path": "./"}, 80); got != "*.go" {
		t.Fatalf("glob root scope leaked: %q", got)
	}
	if got := summarizeToolArgs("glob", map[string]any{"pattern": "*_test.go", "path": "internal"}, 80); got != "*_test.go  · internal" {
		t.Fatalf("glob pattern spelling not summarized: %q", got)
	}
}
