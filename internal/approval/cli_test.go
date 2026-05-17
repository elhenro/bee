package approval

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCLI_AcceptsFirstChar(t *testing.T) {
	cases := []struct {
		in   string
		want Decision
	}{
		{"a\n", AllowOnce},
		{"y\n", AllowOnce},
		{"yes\n", AllowOnce},
		{"s\n", AllowSession},
		{"f\n", AllowAlways},
		{"d\n", Deny},
		{"n\n", Deny},
		{"\n", Deny},
		{"q\n", Deny},  // unknown -> deny
		{"", Deny},      // EOF -> deny
	}
	for _, tc := range cases {
		t.Run(strings.TrimSpace(tc.in)+"_->_"+decisionName(tc.want), func(t *testing.T) {
			var out bytes.Buffer
			c := NewCLI(strings.NewReader(tc.in), &out)
			got, err := c.Request(context.Background(), "rm -rf x", "rm-recursive", "recursive delete")
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("input %q -> %v, want %v", tc.in, got, tc.want)
			}
			if !strings.Contains(out.String(), "recursive delete") {
				t.Errorf("prompt missing reason: %q", out.String())
			}
		})
	}
}

func TestCLI_TruncatesLongCommand(t *testing.T) {
	cmd := strings.Repeat("x", 500)
	var out bytes.Buffer
	c := NewCLI(strings.NewReader("d\n"), &out)
	c.Request(context.Background(), cmd, "k", "d")
	if strings.Contains(out.String(), strings.Repeat("x", 500)) {
		t.Fatal("expected command to be truncated")
	}
	if !strings.Contains(out.String(), "...") {
		t.Fatal("expected truncation marker")
	}
}

func decisionName(d Decision) string {
	switch d {
	case AllowOnce:
		return "AllowOnce"
	case AllowSession:
		return "AllowSession"
	case AllowAlways:
		return "AllowAlways"
	default:
		return "Deny"
	}
}
