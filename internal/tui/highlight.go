package tui

import (
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// highlighter is a lexer, formatter and style resolved once, so a card of rows
// pays the lookup (registry, env, content analysis) once and not per row. nil
// means no lexer matched and every method hands the text back unchanged.
type highlighter struct {
	lex   chroma.Lexer
	fmt   chroma.Formatter
	style *chroma.Style
}

// newHighlighter resolves lang; sample drives content detection when lang
// names no lexer. nil when nothing matches.
func newHighlighter(lang, sample string) *highlighter {
	var lex chroma.Lexer
	if lang != "" {
		lex = lexers.Get(lang)
	}
	if lex == nil {
		lex = lexers.Analyse(sample)
	}
	if lex == nil {
		return nil
	}
	return &highlighter{lex: chroma.Coalesce(lex), fmt: hlFormatter(), style: hlStyle()}
}

// format returns content with ANSI escapes around tokens, unchanged on any
// failure; never errors out of a render path
func (h *highlighter) format(content string) string {
	if h == nil || content == "" {
		return content
	}
	it, err := h.lex.Tokenise(nil, content)
	if err != nil {
		return content
	}
	var b strings.Builder
	if err := h.fmt.Format(&b, h.style, it); err != nil {
		return content
	}
	out := b.String()
	// some lexers append a newline of their own; a row must not grow one
	if !strings.HasSuffix(content, "\n") {
		out = strings.TrimSuffix(out, "\n")
	}
	return out
}

// HighlightCode returns content with ANSI syntax-highlight escapes wrapped
// around tokens. lang is a chroma lexer name ("go", "javascript", "bash",
// "diff"…) or "" to auto-detect by content. On any failure it returns the
// input unchanged — never errors out of a render path.
func HighlightCode(content, lang string) string {
	if content == "" {
		return content
	}
	return newHighlighter(lang, content).format(content)
}

// langFromPath maps a filename to a chroma lexer name. Empty string falls
// back to chroma's content analyser inside HighlightCode.
func langFromPath(path string) string {
	if path == "" {
		return ""
	}
	if lex := lexers.Match(filepath.Base(path)); lex != nil {
		c := lex.Config()
		if c != nil && len(c.Aliases) > 0 {
			return c.Aliases[0]
		}
		if c != nil {
			return strings.ToLower(c.Name)
		}
	}
	return ""
}
