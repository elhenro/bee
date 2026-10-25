package tui

import (
	"github.com/charmbracelet/glamour/ansi"
)

// hex values per terminal background, mirroring the lipgloss palette
type mdTheme struct {
	body, bright, subtle, faint, honey, code, link, rule string
	kw, typ, fn, str, num, comment, op                   string
	tag, del, ins                                        string // markup tags, removed and added lines
}

var mdDark = mdTheme{
	body: "#E0DCDE", bright: "#FFFAF1", subtle: "#8B8997", faint: "#5F5E6A",
	honey: "#FFB000", code: "#7FD1C2", link: "#7AB7FF", rule: "#3A3943",
	kw: "#C69CF5", typ: "#E6C07B", fn: "#7FB4F0", str: "#A6D189", num: "#F2A67C",
	comment: "#6F6E7C", op: "#9A98A6", tag: "#E6838F", del: "#EE6A82", ins: "#6FD6A0",
}

var mdLight = mdTheme{
	body: "#222127", bright: "#17161B", subtle: "#65636F", faint: "#8B8996",
	honey: "#A56F00", code: "#0E7A6C", link: "#0B63C5", rule: "#D2CEC6",
	kw: "#7A3FB8", typ: "#8A5A00", fn: "#1B5FB8", str: "#33761F", num: "#B5541F",
	comment: "#8B8996", op: "#65636F", tag: "#B02A45", del: "#C0203F", ins: "#0A7A4F",
}

func sp(s string) *string { return &s }
func bp(b bool) *bool     { return &b }
func up(u uint) *uint     { return &u }

// glamour style: no hashes on headings, inline code as colored text (no
// slab), bare code blocks, zero document margin so our gutter owns the edge
func markdownStyle(dark bool) ansi.StyleConfig {
	t := mdLight
	if dark {
		t = mdDark
	}
	prim := func(color string) ansi.StylePrimitive { return ansi.StylePrimitive{Color: sp(color)} }
	heading := func(color string) ansi.StyleBlock {
		return ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: sp(color), Bold: bp(true)}}
	}
	return ansi.StyleConfig{
		Document:  ansi.StyleBlock{StylePrimitive: prim(t.body)},
		Paragraph: ansi.StyleBlock{},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: sp(t.subtle), Italic: bp(true)},
			Indent:         up(1),
			IndentToken:    sp("▎ "),
		},
		List: ansi.StyleList{LevelIndent: 2},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{BlockSuffix: "\n", Bold: bp(true)},
		},
		H1: heading(t.honey),
		H2: heading(t.honey),
		H3: heading(t.bright),
		H4: heading(t.bright),
		H5: heading(t.subtle),
		H6: heading(t.subtle),

		Strikethrough:  ansi.StylePrimitive{CrossedOut: bp(true)},
		Emph:           ansi.StylePrimitive{Italic: bp(true)},
		Strong:         ansi.StylePrimitive{Bold: bp(true), Color: sp(t.bright)},
		HorizontalRule: ansi.StylePrimitive{Color: sp(t.rule), Format: "\n──────────────\n"},

		Item:        ansi.StylePrimitive{BlockPrefix: "• "},
		Enumeration: ansi.StylePrimitive{BlockPrefix: ". ", Color: sp(t.subtle)},
		Task:        ansi.StyleTask{Ticked: "[✓] ", Unticked: "[ ] "},

		Link:      ansi.StylePrimitive{Color: sp(t.subtle), Underline: bp(true)},
		LinkText:  ansi.StylePrimitive{Color: sp(t.link), Underline: bp(true)},
		Image:     ansi.StylePrimitive{Color: sp(t.subtle), Underline: bp(true)},
		ImageText: ansi.StylePrimitive{Color: sp(t.subtle), Format: "image: {{.text}}"},

		Code: ansi.StyleBlock{StylePrimitive: prim(t.code)},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: prim(t.body),
				Margin:         up(2),
			},
			Chroma: &ansi.Chroma{
				Comment:          ansi.StylePrimitive{Color: sp(t.comment), Italic: bp(true)},
				CommentPreproc:   prim(t.op),
				Keyword:          prim(t.kw),
				KeywordReserved:  prim(t.kw),
				KeywordNamespace: prim(t.kw),
				KeywordType:      prim(t.typ),
				Operator:         prim(t.op),
				Punctuation:      prim(t.op),
				NameBuiltin:      prim(t.fn),
				NameTag:          prim(t.typ),
				NameAttribute:    prim(t.typ),
				NameClass:        prim(t.typ),
				NameConstant:     prim(t.num),
				NameDecorator:    prim(t.typ),
				NameFunction:     prim(t.fn),
				LiteralNumber:    prim(t.num),
				LiteralString:    prim(t.str),
				GenericDeleted:   prim("#EE6A82"),
				GenericInserted:  prim("#6FD6A0"),
			},
		},
		Table: ansi.StyleTable{
			CenterSeparator: sp("┼"),
			ColumnSeparator: sp("│"),
			RowSeparator:    sp("─"),
		},
		DefinitionDescription: ansi.StylePrimitive{BlockPrefix: "\n  "},
	}
}
