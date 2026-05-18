package main

import (
	"testing"

	"github.com/elhenro/bee/internal/zzz"
)

func TestParseSteerLine(t *testing.T) {
	cases := []struct {
		in       string
		wantKind string
		wantText string
	}{
		{"/stop", zzz.SteerStop, ""},
		{"/quit", zzz.SteerStop, ""},
		{"/abort", zzz.SteerAbort, ""},
		{"/kill", zzz.SteerAbort, ""},
		{"/note hello world", zzz.SteerNote, "hello world"},
		{"/say focus on tests", zzz.SteerNote, "focus on tests"},
		{"hello", zzz.SteerNote, "hello"},
		// unknown slash command falls back to note (preserves the leading slash).
		{"/foo bar", zzz.SteerNote, "/foo bar"},
	}
	for _, c := range cases {
		got := parseSteerLine(c.in)
		if got.Kind != c.wantKind {
			t.Errorf("parseSteerLine(%q): kind=%q want %q", c.in, got.Kind, c.wantKind)
		}
		if got.Text != c.wantText {
			t.Errorf("parseSteerLine(%q): text=%q want %q", c.in, got.Text, c.wantText)
		}
	}
}
