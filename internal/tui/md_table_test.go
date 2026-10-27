package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const smallTable = "| Check | Result | Notes |\n|-------|--------|-------|\n" +
	"| `seq 1 40` | ok | 40 lines |\n| `ls /nope` | **fail** | no such dir |"

func TestSplitTables_FindsTablesOutsideFences(t *testing.T) {
	src := "intro\n\n" + smallTable + "\n\noutro\n\n```\n| a | b |\n|---|---|\n```\n"
	segs := splitTables(src)
	if len(segs) != 3 || segs[0].table || !segs[1].table || segs[2].table {
		t.Fatalf("want prose, table, prose; got %+v", segs)
	}
	if !strings.HasPrefix(segs[1].text, "| Check") || !strings.HasSuffix(segs[1].text, "no such dir |") {
		t.Fatalf("table segment boundaries wrong: %q", segs[1].text)
	}
	if !strings.Contains(segs[2].text, "```\n| a | b |") {
		t.Fatalf("table inside a fence was split out: %q", segs[2].text)
	}
	if got := splitTables("no table, just a | pipe\n---\n"); len(got) != 1 || got[0].table {
		t.Fatalf("a stray pipe made a table: %+v", got)
	}
}

func TestSplitRow_RespectsCodeSpansAndEscapes(t *testing.T) {
	got := splitRow("| a `x|y` | b \\| c | d |")
	if len(got) != 3 || strings.TrimSpace(got[0]) != "a `x|y`" || !strings.Contains(got[1], `\|`) {
		t.Fatalf("cells = %q", got)
	}
}

func TestTableWidth_CountsWidestCellPerColumn(t *testing.T) {
	// cols 3 and 2 wide, a margin either side of each, a rule between, two slack cells
	if got := tableWidth("| a | bb |\n|---|---|\n| ccc | d |"); got != (3+2)+(2+2)+1+2 {
		t.Fatalf("width = %d", got)
	}
	// markers and link syntax are measured as they will read
	got := tableWidth("| h |\n|---|\n| `x` |\n| [t](http://u) |")
	if want := len("t http://u") + 2 + 2; got != want {
		t.Fatalf("link cell width = %d, want %d", got, want)
	}
}

func TestRenderText_TableFitsItsContent(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 120)
	out := ansi.Strip(r.renderText("Findings:\n\n" + smallTable + "\n\nDone."))
	lines := strings.Split(out, "\n")
	widest := 0
	for _, ln := range lines {
		widest = max(widest, ansi.StringWidth(strings.TrimRight(ln, " ")))
	}
	if widest > 50 {
		t.Fatalf("table still stretches (%d cells):\n%s", widest, out)
	}
	for _, want := range []string{"Findings:", "Check", "seq 1 40", "fail", "no such dir", "Done."} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	for _, ln := range lines {
		if strings.Contains(ln, "seq 1 40") && !strings.Contains(ln, "40 lines") {
			t.Fatalf("row wrapped across lines: %q", ln)
		}
	}
}

func TestRenderText_WideTableStillWrapsInsideTheTerminal(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 60)
	long := strings.Repeat("word ", 20)
	out := ansi.Strip(r.renderText("| a | b |\n|---|---|\n| " + long + " | " + long + " |"))
	for ln := range strings.SplitSeq(out, "\n") {
		if w := ansi.StringWidth(ln); w > 60 {
			t.Fatalf("row is %d cells in a 60 cell terminal: %q", w, ln)
		}
	}
}
