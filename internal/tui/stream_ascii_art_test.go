package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/tools/read"
)

// two labelled pictures, 12 rows each, one bright (blank) row in the second
func artOutput() string {
	var b strings.Builder
	for _, name := range []string{"a.jpeg", "b.jpeg"} {
		fmt.Fprintf(&b, "== public/images/%s\n", name)
		for i := 0; i < 12; i++ {
			switch {
			case name == "b.jpeg" && i == 5:
				b.WriteString(strings.Repeat(" ", 40) + "\n")
			case i%2 == 0:
				b.WriteString(strings.Repeat(":", 30) + strings.Repeat("R", 15) + strings.Repeat(":", 10) + "\n")
			default:
				b.WriteString(strings.Repeat(":", 25) + strings.Repeat("R", 14) + strings.Repeat(":", 16) + "\n")
			}
		}
	}
	return b.String()
}

func TestToolCard_AsciiArtFoldsToSummary(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 100)
	r.RenderMessage(useMsg("u1", "bash", map[string]any{"command": "python render.py"}))
	out := stripANSI(r.RenderMessage(resultMsg("u1", artOutput(), false)))
	for _, want := range []string{"== public/images/a.jpeg", "== public/images/b.jpeg", "ascii art " + glyphSep + " 12 rows × 55 cols"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, ":::::") {
		t.Fatalf("picture rows must fold away: %q", out)
	}
	if strings.Contains(out, "+") && strings.Contains(out, "lines") {
		t.Fatalf("folded card fits the preview, no +N lines hint: %q", out)
	}
}

func TestToolCard_AsciiArtVerboseKeepsShape(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 100)
	r.RenderMessage(useMsg("u1", "bash", map[string]any{"command": "python render.py"}))
	full := stripANSI(r.RenderMessageDetail(resultMsg("u1", artOutput(), false), true))
	rows := 0
	for _, l := range strings.Split(full, "\n") {
		if strings.Contains(l, "RRRR") {
			rows++
		}
	}
	if rows != 23 {
		t.Fatalf("verbose keeps all 23 art rows, got %d in %q", rows, full)
	}
	if !strings.Contains(full, "\n\n") {
		t.Fatalf("verbose keeps the bright blank row inside the picture: %q", full)
	}
}

func TestFoldArt_LeavesOrdinaryOutput(t *testing.T) {
	cases := map[string]string{
		"prose":   numbered(20, "this is an ordinary log line number"),
		"short":   strings.Repeat(strings.Repeat(":", 40)+"\n", artMinRows-1),
		"narrow":  strings.Repeat("::::\n", 20),
		"table":   strings.Repeat("| name        | value       |\n", 12),
		"mixed":   strings.Repeat("abcdefghijklmnopqrstuvwxyz0123456789\n", 12),
		"divider": "ok\n" + strings.Repeat("-", 60) + "\nok\n",
	}
	for name, in := range cases {
		got := foldArt(in, false)
		if want := compactLines(in); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s: folded %d rows to %d: %q", name, len(want), len(got), got)
		}
	}
}

func TestFoldArt_ReadResultsStayLineCounted(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 100)
	r.RenderMessage(useMsg("u1", "read", map[string]any{"path": "art.txt"}))
	out := stripANSI(r.RenderMessage(resultMsg("u1", strings.Repeat(strings.Repeat(":", 40)+"\n", 20), false)))
	if !strings.Contains(out, "20 lines") {
		t.Fatalf("read keeps its line count: %q", out)
	}
}

// a read that attached a picture names it instead of "1 line"
func TestToolCard_ReadImageShowsNote(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 100)
	r.RenderMessage(useMsg("u1", "read", map[string]any{"path": "public/images/ass1.jpeg"}))
	note := read.NotePrefix + "ass1.jpeg · image/jpeg · 1200×800 · 120 KB · attached below"
	out := stripANSI(r.RenderMessage(resultMsg("u1", note, false)))
	if !strings.Contains(out, "ass1.jpeg · image/jpeg · 1200×800") || strings.Contains(out, "1 line") {
		t.Fatalf("want the image note, got %q", out)
	}
}
