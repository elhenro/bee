package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/elhenro/bee/internal/types"
)

func TestMarkdownStyle_HeadingsLoseHashesInlineCodeLosesSlab(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	r := NewStreamRenderer(DefaultStyles(), 80)
	raw := r.renderText("## Title\n\nrun `go test` now")
	if strings.Contains(stripANSI(raw), "#") {
		t.Fatalf("headings must not keep their hashes: %q", stripANSI(raw))
	}
	if !strings.Contains(stripANSI(raw), "Title") || !strings.Contains(stripANSI(raw), "go test") {
		t.Fatalf("content lost: %q", stripANSI(raw))
	}
	// inline code is colored text, never a background slab
	if strings.Contains(raw, "\x1b[48;") {
		t.Fatalf("inline code must not paint a background: %q", raw)
	}
}

func TestMarkdownStyle_DarkAndLightDiffer(t *testing.T) {
	d, l := markdownStyle(true), markdownStyle(false)
	if *d.Code.Color == *l.Code.Color || *d.Document.Color == *l.Document.Color {
		t.Fatal("light and dark themes must use different hex values")
	}
	if d.CodeBlock.Chroma == nil || d.CodeBlock.Chroma.Keyword.Color == nil {
		t.Fatal("code blocks need explicit token colors")
	}
}

func TestSplitSettled(t *testing.T) {
	cases := []struct {
		name, in, settled, tail string
	}{
		{"one paragraph", "a", "", "a"},
		{"two", "a\n\nb", "a", "b"},
		{"three", "a\n\nb\n\nc", "a\n\nb", "c"},
		{"trailing break", "a\n\n", "a", ""},
		{"extra blanks", "a\n\n\nb", "a", "\nb"},
		{"blank inside fence is not a break", "```go\nx\n\ny\n```\n\nz", "```go\nx\n\ny\n```", "z"},
		{"open fence stays in the tail", "a\n\n```go\nx\n\ny", "a", "```go\nx\n\ny"},
		{"nothing", "", "", ""},
	}
	for _, c := range cases {
		s, tl := splitSettled(c.in)
		if s != c.settled || tl != c.tail {
			t.Errorf("%s: splitSettled(%q) = (%q, %q), want (%q, %q)", c.name, c.in, s, tl, c.settled, c.tail)
		}
	}
}

func TestStreamMarkdown_StylesSettledKeepsTailRaw(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	out := stripANSI(r.streamMarkdown("## Done\n\nthe **tail** is still raw"))
	if strings.Contains(out, "##") || !strings.Contains(out, "Done") {
		t.Fatalf("settled heading should be styled: %q", out)
	}
	if !strings.Contains(out, "**tail**") {
		t.Fatalf("unfinished paragraph stays raw: %q", out)
	}
	if !strings.Contains(out, "Done\n\nthe") {
		t.Fatalf("settled and tail keep one blank row between them: %q", out)
	}
}

func TestStreamMarkdown_CachesSettledRender(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	r.streamMarkdown("## One\n\npara tail")
	src, out := r.settledSrc, r.settledOut
	if src == "" || out == "" {
		t.Fatal("settled paragraph should be cached")
	}
	// only the tail grows: the settled render is reused, not redone
	r.settledOut = "SENTINEL"
	r.streamMarkdown("## One\n\npara tail keeps growing")
	if r.settledOut != "SENTINEL" {
		t.Fatal("settled prefix re-rendered although unchanged")
	}
	// a new paragraph lands: cache refreshes
	r.streamMarkdown("## One\n\npara done\n\nnext")
	if r.settledOut == "SENTINEL" {
		t.Fatal("cache must refresh when the settled prefix changes")
	}
}

func TestRenderStreaming_SettledParagraphsMatchCommittedStyle(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	text := "## Done\n\nthe server has `ReadTimeout` set.\n\ntail"
	live := stripANSI(r.RenderStreaming(text, 0))
	if strings.Contains(live, "`") || strings.Contains(live, "##") {
		t.Fatalf("live view should show settled paragraphs styled, got %q", live)
	}
}

func TestRenderMessage_ContinuationHangsFirstBlockOnly(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	r.SetContinuation(true)
	out := stripANSI(r.RenderMessage(types.Message{Role: types.RoleAssistant, Content: []types.ContentBlock{
		{Type: types.BlockText, Text: "rest of the reply"},
		{Type: types.BlockText, Text: "second block"},
	}}))
	if strings.Contains(out, glyphBee+" rest of the reply") {
		t.Fatalf("continuation must not repeat the marker: %q", out)
	}
	if !strings.Contains(out, glyphBee+" second block") {
		t.Fatalf("a later block starts a fresh marker: %q", out)
	}
}

func TestRenderText_TrimsBlankEdgesAndTabs(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	out := r.renderText("```go\n\tx := 1\n```")
	if strings.Contains(out, "\t") {
		t.Fatalf("tabs must be expanded before they reach the terminal: %q", out)
	}
	rows := strings.Split(out, "\n")
	if strings.TrimSpace(stripANSI(rows[0])) == "" || strings.TrimSpace(stripANSI(rows[len(rows)-1])) == "" {
		t.Fatalf("blank edge rows should be trimmed: %q", out)
	}
}
