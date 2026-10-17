package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestTextWidth_AccountsForGutterIndentAndMargin(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	// clean: gutter(1) + indent + right margin(1)
	if got := r.textWidth(markIndent); got != 80-2-1-1 {
		t.Fatalf("clean textWidth(%d) = %d, want %d", markIndent, got, 80-markIndent-2)
	}
	r.SetCompact(true) // no gutter
	if got := r.textWidth(markIndent); got != 80-markIndent-1 {
		t.Fatalf("compact textWidth = %d, want %d", got, 80-markIndent-1)
	}
	tiny := NewStreamRenderer(DefaultStyles(), 40)
	tiny.width = 5
	if got := tiny.textWidth(markIndent); got != 20 {
		t.Fatalf("textWidth floors at 20, got %d", got)
	}
}

func TestHang_IndentsContinuationOnly(t *testing.T) {
	if got := hang("a\nb\n\nc", "> ", "  "); got != "> a\n  b\n\n  c" {
		t.Fatalf("hang = %q", got)
	}
	if got := hang("", "> ", "  "); got != "> " {
		t.Fatalf("hang(empty) = %q", got)
	}
}

func TestWrapText_BreaksOnWordsThenHard(t *testing.T) {
	got := wrapText("one two three four", 9)
	for _, ln := range strings.Split(got, "\n") {
		if lipgloss.Width(ln) > 9 {
			t.Fatalf("row %q wider than 9", ln)
		}
	}
	if !strings.Contains(got, "\n") || strings.Contains(strings.ReplaceAll(got, "\n", ""), "thre e") {
		t.Fatalf("expected word-boundary wrap, got %q", got)
	}
	long := wrapText(strings.Repeat("x", 25), 10)
	for _, ln := range strings.Split(long, "\n") {
		if lipgloss.Width(ln) > 10 {
			t.Fatalf("hard wrap left a %d-wide row: %q", lipgloss.Width(ln), ln)
		}
	}
}

func TestPadToAndClipLine(t *testing.T) {
	if got := padTo("ab", 5); got != "ab   " {
		t.Fatalf("padTo = %q", got)
	}
	if got := padTo("abcdef", 3); got != "abcdef" {
		t.Fatalf("padTo must never truncate, got %q", got)
	}
	got := clipLine("abcdefghij", 6)
	if lipgloss.Width(got) > 6 || !strings.HasSuffix(got, glyphMore) {
		t.Fatalf("clipLine = %q", got)
	}
	if got := clipLine("abc", 6); got != "abc" {
		t.Fatalf("short line must pass through, got %q", got)
	}
}

func TestShortCwd(t *testing.T) {
	if got := shortCwd("~/projects-new/bee", 32); got != "~/projects-new/bee" {
		t.Fatalf("short path untouched, got %q", got)
	}
	long := "/Users/someone/projects-new/deeply/nested/bee"
	got := shortCwd(long, 20)
	if got != "/Users/…/nested/bee" {
		t.Fatalf("shortCwd = %q", got)
	}
}
