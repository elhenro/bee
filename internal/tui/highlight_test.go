package tui

import (
	"strings"
	"testing"
)

func TestHighlighter_NilHandsTextBack(t *testing.T) {
	var h *highlighter
	if got := h.format("plain"); got != "plain" {
		t.Fatalf("nil highlighter must pass text through, got %q", got)
	}
	if h := newHighlighter("no-such-lexer", ""); h != nil {
		t.Fatal("unknown lexer and nothing to sniff must resolve to nil")
	}
}

func TestHighlighter_ReuseMatchesHighlightCode(t *testing.T) {
	h := newHighlighter("go", "")
	for _, ln := range []string{"func main() {}", "x := 1", "// note"} {
		if got, want := h.format(ln), HighlightCode(ln, "go"); got != want {
			t.Fatalf("shared lexer differs for %q: %q vs %q", ln, got, want)
		}
	}
}

func TestHighlighter_RowNeverGrowsANewline(t *testing.T) {
	if got := HighlightCode("+added", "diff"); strings.HasSuffix(got, "\n") {
		t.Fatalf("lexer-appended newline leaked into a row: %q", got)
	}
}

// a block comment spans rows; highlighted as one document the second row is
// still a comment, which a lexer fed row by row cannot know
func TestHlRows_KeepsMultiLineConstructsAndRowCount(t *testing.T) {
	t.Setenv("COLORTERM", "truecolor")
	r := NewStreamRenderer(DefaultStyles(), 100)
	rows := []string{"/* block", "func inside() {}", "*/", "func main() {}"}
	out := r.hlRows(rows, "a.go")
	if len(out) != len(rows) {
		t.Fatalf("want %d rows, got %d: %q", len(rows), len(out), out)
	}
	if got := stripANSI(strings.Join(out, "\n")); got != strings.Join(rows, "\n") {
		t.Fatalf("highlighting must not change the text: %q", got)
	}
	perRow := HighlightCode(rows[1], "go")
	if out[1] == perRow {
		t.Fatalf("row inside the comment is styled as code: %q", out[1])
	}
	if out[3] != HighlightCode(rows[3], "go") {
		t.Fatalf("code after the comment loses its colors: %q", out[3])
	}
}

func TestIsFenceLine(t *testing.T) {
	for ln, want := range map[string]bool{
		"```":        true,
		"```go":      true,
		"  ```bash":  true,
		"\t```":      true,
		"text ```":   false,
		"``":         false,
		"":           false,
		"`inline`":   false,
		"~~~ not us": false,
	} {
		if got := isFenceLine(ln); got != want {
			t.Errorf("isFenceLine(%q) = %v, want %v", ln, got, want)
		}
	}
}
