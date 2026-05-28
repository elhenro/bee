package remote

import (
	"encoding/json"
	"io"
	"strings"
)

// writeSSE emits one event in `event: <type>\ndata: <json>\n\n` form.
func writeSSE(w io.Writer, typ string, data interface{}) {
	payload, err := json.Marshal(data)
	if err != nil {
		payload = []byte("null")
	}
	io.WriteString(w, "event: ")
	io.WriteString(w, typ)
	io.WriteString(w, "\ndata: ")
	w.Write(payload)
	io.WriteString(w, "\n\n")
}

// htmlEscape escapes the few chars that matter inside the title.
func htmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
	)
	return r.Replace(s)
}
