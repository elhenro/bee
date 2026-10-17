package tui

import (
	"os"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/charmbracelet/lipgloss"
)

// muted token colors that sit next to honey; no background entries so code
// never paints a slab
var (
	hlDark = chroma.MustNewStyle("bee-dark", chroma.StyleEntries{
		chroma.Comment:             "#6F6E7C italic",
		chroma.CommentPreproc:      "#9A98A6",
		chroma.Keyword:             "#C69CF5",
		chroma.KeywordType:         "#E6C07B",
		chroma.KeywordConstant:     "#F2A67C",
		chroma.Operator:            "#9A98A6",
		chroma.Punctuation:         "#9A98A6",
		chroma.NameFunction:        "#7FB4F0",
		chroma.NameBuiltin:         "#7FB4F0",
		chroma.NameClass:           "#E6C07B",
		chroma.NameDecorator:       "#E6C07B",
		chroma.NameAttribute:       "#E6C07B",
		chroma.NameTag:             "#E6838F",
		chroma.NameConstant:        "#F2A67C",
		chroma.LiteralString:       "#A6D189",
		chroma.LiteralStringEscape: "#7FD1C2",
		chroma.LiteralNumber:       "#F2A67C",
		chroma.GenericDeleted:      "#EE6A82",
		chroma.GenericInserted:     "#6FD6A0",
		chroma.GenericHeading:      "bold",
		chroma.GenericSubheading:   "#C69CF5",
		chroma.GenericEmph:         "italic",
		chroma.GenericStrong:       "bold",
		chroma.Error:               "#EE6A82",
	})
	hlLight = chroma.MustNewStyle("bee-light", chroma.StyleEntries{
		chroma.Comment:             "#8B8996 italic",
		chroma.CommentPreproc:      "#65636F",
		chroma.Keyword:             "#7A3FB8",
		chroma.KeywordType:         "#8A5A00",
		chroma.KeywordConstant:     "#B5541F",
		chroma.Operator:            "#65636F",
		chroma.Punctuation:         "#65636F",
		chroma.NameFunction:        "#1B5FB8",
		chroma.NameBuiltin:         "#1B5FB8",
		chroma.NameClass:           "#8A5A00",
		chroma.NameDecorator:       "#8A5A00",
		chroma.NameAttribute:       "#8A5A00",
		chroma.NameTag:             "#B02A45",
		chroma.NameConstant:        "#B5541F",
		chroma.LiteralString:       "#33761F",
		chroma.LiteralStringEscape: "#0E7A6C",
		chroma.LiteralNumber:       "#B5541F",
		chroma.GenericDeleted:      "#C0203F",
		chroma.GenericInserted:     "#0A7A4F",
		chroma.GenericHeading:      "bold",
		chroma.GenericSubheading:   "#7A3FB8",
		chroma.GenericEmph:         "italic",
		chroma.GenericStrong:       "bold",
		chroma.Error:               "#C0203F",
	})
)

// style matching the terminal background lipgloss resolved at startup
func hlStyle() *chroma.Style {
	if lipgloss.HasDarkBackground() {
		return hlDark
	}
	return hlLight
}

// 24-bit only where advertised; legacy terms render those as garbage
func hlFormatter() chroma.Formatter {
	ct := strings.ToLower(os.Getenv("COLORTERM"))
	if ct == "truecolor" || ct == "24bit" {
		return formatters.Get("terminal16m")
	}
	return formatters.Get("terminal256")
}
