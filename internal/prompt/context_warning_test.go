package prompt

import (
	"strings"
	"testing"
)

func TestFormatContextWarning(t *testing.T) {
	cases := []struct {
		name       string
		in, limit  int
		wantEmpty  bool
		wantSubstr string
	}{
		{"below threshold", 60, 100, true, ""},
		{"at exactly 70 percent", 70, 100, false, "[context at 70%]"},
		{"73 percent", 73, 100, false, "73%"},
		{"inflated 200 percent clamped", 200, 100, false, "[context at 100%]"},
		{"zero limit", 80, 0, true, ""},
		{"negative input", -5, 100, true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := FormatContextWarning(c.in, c.limit)
			if c.wantEmpty {
				if got != "" {
					t.Fatalf("want empty, got %q", got)
				}
				return
			}
			if !strings.Contains(got, c.wantSubstr) {
				t.Fatalf("want substr %q, got %q", c.wantSubstr, got)
			}
			if !strings.HasSuffix(got, "\n\n") {
				t.Fatalf("want trailing double newline, got %q", got)
			}
			if strings.Contains(got, "200%") {
				t.Fatalf("raw inflated number leaked: %q", got)
			}
		})
	}
}
