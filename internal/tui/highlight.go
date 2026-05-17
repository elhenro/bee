package tui

import (
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// chroma style + formatter pinned at startup. terminal256 is the widest
// safe target (most macOS/linux TTYs); the truecolor formatter clashes with
// some legacy terms by emitting 24-bit sequences they render as garbage.
// "monokai" stays readable on both light and dark backgrounds.
var (
	hlStyle     = styles.Get("monokai")
	hlFormatter = formatters.Get("terminal256")
)

// HighlightCode returns content with ANSI syntax-highlight escapes wrapped
// around tokens. lang is a chroma lexer name ("go", "javascript", "bash",
// "diff"…) or "" to auto-detect by content. On any failure it returns the
// input unchanged — never errors out of a render path.
func HighlightCode(content, lang string) string {
	if content == "" || hlStyle == nil || hlFormatter == nil {
		return content
	}
	var lex chroma.Lexer
	if lang != "" {
		lex = lexers.Get(lang)
	}
	if lex == nil {
		lex = lexers.Analyse(content)
	}
	if lex == nil {
		return content
	}
	lex = chroma.Coalesce(lex)
	it, err := lex.Tokenise(nil, content)
	if err != nil {
		return content
	}
	var b strings.Builder
	if err := hlFormatter.Format(&b, hlStyle, it); err != nil {
		return content
	}
	return b.String()
}

// HighlightLineByPath highlights a single line using the lexer matching the
// given file path's extension. Used by per-line render paths (diff bodies,
// edit previews) that hand chroma one logical line at a time.
func HighlightLineByPath(line, path string) string {
	if line == "" {
		return line
	}
	return HighlightCode(line, langFromPath(path))
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
