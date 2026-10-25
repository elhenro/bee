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
	hlDark  = hlTheme("bee-dark", mdDark)
	hlLight = hlTheme("bee-light", mdLight)
)

func hlTheme(name string, t mdTheme) *chroma.Style {
	return chroma.MustNewStyle(name, chroma.StyleEntries{
		chroma.Comment:             t.comment + " italic",
		chroma.CommentPreproc:      t.op,
		chroma.Keyword:             t.kw,
		chroma.KeywordType:         t.typ,
		chroma.KeywordConstant:     t.num,
		chroma.Operator:            t.op,
		chroma.Punctuation:         t.op,
		chroma.NameFunction:        t.fn,
		chroma.NameBuiltin:         t.fn,
		chroma.NameClass:           t.typ,
		chroma.NameDecorator:       t.typ,
		chroma.NameAttribute:       t.typ,
		chroma.NameTag:             t.tag,
		chroma.NameConstant:        t.num,
		chroma.LiteralString:       t.str,
		chroma.LiteralStringEscape: t.code,
		chroma.LiteralNumber:       t.num,
		chroma.GenericDeleted:      t.del,
		chroma.GenericInserted:     t.ins,
		chroma.GenericHeading:      "bold",
		chroma.GenericSubheading:   t.kw,
		chroma.GenericEmph:         "italic",
		chroma.GenericStrong:       "bold",
		chroma.Error:               t.del,
	})
}

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
