package web_fetch

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// htmlToMarkdown converts HTML content to markdown
func htmlToMarkdown(htmlContent []byte) (string, error) {
	doc, err := html.Parse(bytes.NewReader(htmlContent))
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %w", err)
	}

	var buf strings.Builder
	// Start from the body element (skip html/head wrappers)
	if body := findBody(doc); body != nil {
		for c := body.FirstChild; c != nil; c = c.NextSibling {
			renderNode(c, &buf)
		}
	}
	return normalizeMarkdown(buf.String()), nil
}

// findBody finds the body element in the HTML document
func findBody(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.Data == "body" {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findBody(c); found != nil {
			return found
		}
	}
	return nil
}

// wrappers maps an element to the markdown written before and after its
// rendered children.
var wrappers = map[string][2]string{
	"h1": {"\n\n# ", ""}, "h2": {"\n\n## ", ""}, "h3": {"\n\n### ", ""},
	"h4": {"\n\n#### ", ""}, "h5": {"\n\n##### ", ""}, "h6": {"\n\n###### ", ""},
	"p": {"\n\n", ""}, "br": {"\n", ""},
	"strong": {"**", "**"}, "b": {"**", "**"},
	"em": {"*", "*"}, "i": {"*", "*"},
	"ul": {"\n\n", ""}, "ol": {"\n\n", ""}, "li": {"\n- ", ""},
	"blockquote": {"\n\n> ", ""},
	"code":       {"`", "`"},
}

// renderNode renders a single HTML node to markdown
func renderNode(n *html.Node, w *strings.Builder) {
	if n.Type == html.TextNode {
		text := strings.TrimSpace(n.Data)
		if text != "" {
			w.WriteString(text)
		} else if strings.ContainsAny(n.Data, " \t\n\r") {
			// Preserve single space for whitespace-only text nodes (e.g., between inline elements)
			w.WriteString(" ")
		}
		return
	}
	if n.Type != html.ElementNode || n.Data == "script" || n.Data == "style" {
		return
	}

	var children strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		renderNode(c, &children)
	}
	childText := children.String()

	if wrap, ok := wrappers[n.Data]; ok {
		w.WriteString(wrap[0] + childText + wrap[1])
		return
	}
	switch n.Data {
	case "a":
		if href := getAttr(n, "href"); href != "" {
			w.WriteString("[" + childText + "](" + href + ")")
		} else {
			w.WriteString(childText)
		}
	case "pre":
		// a code child supplies the raw text nodes; otherwise use the rendered children
		text := childText
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "code" {
				text = rawText(c)
				break
			}
		}
		w.WriteString("\n\n```" + text + "```\n\n")
	case "img":
		if alt := getAttr(n, "alt"); alt != "" {
			w.WriteString("![" + alt + "](" + getAttr(n, "src") + ")")
		}
	default:
		w.WriteString(childText)
	}
}

// rawText concatenates the direct text children of n, unmodified.
func rawText(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	}
	return b.String()
}

// getAttr returns the value of an attribute from a node
func getAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

// normalizeMarkdown trims the text and collapses runs of 3+ newlines to a
// single blank line, preserving markdown structure
func normalizeMarkdown(s string) string {
	var buf strings.Builder
	newlines := 0
	for _, r := range strings.TrimSpace(s) {
		if r == '\n' {
			if newlines++; newlines > 2 {
				continue
			}
		} else {
			newlines = 0
		}
		buf.WriteRune(r)
	}
	return buf.String()
}
