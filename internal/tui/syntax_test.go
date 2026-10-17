package tui

import (
	"strings"
	"testing"

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

func containsBg(s string) bool {
	for i := 0; i+3 < len(s); i++ {
		if s[i] == 0x1b && s[i+1] == '[' && s[i+2] == '4' && s[i+3] == '8' {
			return true
		}
	}
	return false
}
