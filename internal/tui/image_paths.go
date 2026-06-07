package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

const imageLabelMax = 24

var imageExts = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// loadImage loads p as an image block when it points at an existing local image
// file. p should already be backslash-unescaped; loadImage strips surrounding
// quotes and expands ~. Returns the block, its basename, and ok=false otherwise.
func loadImage(p string) (types.ContentBlock, string, bool) {
	p = expandHome(unquote(strings.TrimSpace(p)))
	mt, ok := imageExts[strings.ToLower(filepath.Ext(p))]
	if !ok {
		return types.ContentBlock{}, "", false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return types.ContentBlock{}, "", false
	}
	return types.ContentBlock{Type: types.BlockImage, MediaType: mt, Data: data}, filepath.Base(p), true
}

// imageLabel is the compact scrollback/input token for a loaded image.
func imageLabel(base string) string { return "[Image: " + truncLabel(base) + "]" }

// extractImagePaths scans text for local image-file path tokens — including the
// shell-escaped paths a terminal inserts on drag-and-drop — loads each as an
// image block, and returns the text with every matched path replaced by a
// compact "[Image: name]" label. Non-image / non-existent tokens are left as-is.
func extractImagePaths(text string) (string, []types.ContentBlock) {
	var (
		imgs []types.ContentBlock
		out  []string
	)
	for _, tk := range splitShellLike(text) {
		blk, base, ok := loadImage(tk.unescaped)
		if !ok {
			out = append(out, tk.raw)
			continue
		}
		imgs = append(imgs, blk)
		out = append(out, imageLabel(base))
	}
	return strings.Join(out, " "), imgs
}

// unescape drops a backslash before any following rune — the inverse of the
// shell-style escaping a terminal applies to a dragged path's spaces.
func unescape(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		if rs[i] == '\\' && i+1 < len(rs) {
			i++
		}
		b.WriteRune(rs[i])
	}
	return b.String()
}

type shellToken struct{ raw, unescaped string }

// splitShellLike splits on unescaped whitespace, keeping both the original
// token (raw, escapes intact) and a backslash-unescaped form. Mirrors how a
// terminal hands over a dragged path: spaces arrive as "\ ".
func splitShellLike(s string) []shellToken {
	var toks []shellToken
	var raw, un strings.Builder
	flush := func() {
		if raw.Len() > 0 {
			toks = append(toks, shellToken{raw.String(), un.String()})
			raw.Reset()
			un.Reset()
		}
	}
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		if c == '\\' && i+1 < len(rs) && (rs[i+1] == ' ' || rs[i+1] == '\\') {
			raw.WriteRune(c)
			raw.WriteRune(rs[i+1])
			un.WriteRune(rs[i+1])
			i++
			continue
		}
		if c == ' ' || c == '\t' || c == '\n' {
			flush()
			continue
		}
		raw.WriteRune(c)
		un.WriteRune(c)
	}
	flush()
	return toks
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// truncLabel shortens a filename to imageLabelMax runes, ellipsizing the middle
// of the tail so the extension stays visible.
func truncLabel(name string) string {
	r := []rune(name)
	if len(r) <= imageLabelMax {
		return name
	}
	return string(r[:imageLabelMax-1]) + "…"
}
