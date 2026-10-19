package web_fetch

import (
	"mime"
	"net/http"
	"strings"
)

// renderBody converts HTML to markdown and passes every other body through
// verbatim: html-parsing source code or JSON drops everything after a "<".
func renderBody(contentType string, body []byte) (string, error) {
	if !isHTML(contentType, body) {
		return strings.TrimSpace(strings.ToValidUTF8(string(body), "")), nil
	}
	return htmlToMarkdown(body)
}

// isHTML trusts the declared media type; with no usable header it sniffs.
func isHTML(contentType string, body []byte) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil || mt == "" {
		return strings.HasPrefix(http.DetectContentType(body), "text/html")
	}
	return mt == "text/html" || mt == "application/xhtml+xml"
}
