package tui

import (
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/lipgloss"
)

func TestHlStyle_FollowsBackground(t *testing.T) {
	prev := lipgloss.HasDarkBackground()
	defer lipgloss.SetHasDarkBackground(prev)
	lipgloss.SetHasDarkBackground(true)
	if hlStyle() != hlDark {
		t.Fatal("dark terminal should get the dark token palette")
	}
	lipgloss.SetHasDarkBackground(false)
	if hlStyle() != hlLight {
		t.Fatal("light terminal should get the light token palette")
	}
}

func TestHlFormatter_TruecolorOnlyWhenAdvertised(t *testing.T) {
	t.Setenv("COLORTERM", "truecolor")
	if out := HighlightCode("func main() {}", "go"); !strings.Contains(out, "38;2;") {
		t.Fatalf("COLORTERM=truecolor should emit 24-bit sequences: %q", out)
	}
	t.Setenv("COLORTERM", "")
	if out := HighlightCode("func main() {}", "go"); !strings.Contains(out, "38;5;") || strings.Contains(out, "38;2;") {
		t.Fatalf("no truecolor advert should fall back to the 256 cube: %q", out)
	}
}

func TestHighlightCode_ColorsGoKeywordsWithoutBackground(t *testing.T) {
	t.Setenv("COLORTERM", "truecolor")
	out := HighlightCode("func main() {}", "go")
	if out == "func main() {}" {
		t.Fatal("expected ANSI-colored tokens")
	}
	if containsBg(out) {
		t.Fatalf("code must never paint a background slab: %q", out)
	}
}

// the token palettes are built from the markdown theme; pin a few entries
func TestHlTheme_TokenColors(t *testing.T) {
	cases := []struct {
		name  string
		style *chroma.Style
		tok   chroma.TokenType
		want  string
	}{
		{"dark keyword", hlDark, chroma.Keyword, "#c69cf5"},
		{"dark tag", hlDark, chroma.NameTag, "#e6838f"},
		{"dark escape", hlDark, chroma.LiteralStringEscape, "#7fd1c2"},
		{"dark deleted", hlDark, chroma.GenericDeleted, "#ee6a82"},
		{"light keyword", hlLight, chroma.Keyword, "#7a3fb8"},
		{"light tag", hlLight, chroma.NameTag, "#b02a45"},
		{"light inserted", hlLight, chroma.GenericInserted, "#0a7a4f"},
		{"light error", hlLight, chroma.Error, "#c0203f"},
	}
	for _, c := range cases {
		if got := c.style.Get(c.tok).Colour.String(); got != c.want {
			t.Errorf("%s = %s, want %s", c.name, got, c.want)
		}
	}
	if hlDark.Get(chroma.Comment).Italic != chroma.Yes {
		t.Error("comments should be italic")
	}
}

func containsBg(s string) bool {
	for i := 0; i+3 < len(s); i++ {
		if s[i] == 0x1b && s[i+1] == '[' && s[i+2] == '4' && s[i+3] == '8' {
			return true
		}
	}
	return false
}
