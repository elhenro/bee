package hashline_edit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/tools/apply_patch"
)

func TestCRLFAndNoTrailingNewlinePreserved(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"crlf", "one\r\ntwo\r\nthree\r\n", "one\r\nTWO\r\n2b\r\nthree\r\n"},
		{"crlf no trailing", "one\r\ntwo\r\nthree", "one\r\nTWO\r\n2b\r\nthree"},
		{"lf no trailing", "one\ntwo\nthree", "one\nTWO\n2b\nthree"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeFile(t, c.in)
			lines := strings.Split(strings.ReplaceAll(c.in, "\r", ""), "\n")
			res, err := New(filepath.Dir(path)).Run(context.Background(), map[string]any{
				"path": path,
				"edits": []any{map[string]any{
					"pos": apply_patch.Ref(lines[1], 2), "op": "replace", "lines": []any{"TWO", "2b"},
				}},
			})
			if err != nil || res.IsError {
				t.Fatalf("edit failed: %v %s", err, res.Content)
			}
			got, _ := os.ReadFile(path)
			if string(got) != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}
