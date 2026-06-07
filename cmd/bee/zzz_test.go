package main

import (
	"testing"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/loop"
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

func TestResolveZzzBudgets(t *testing.T) {
	tiny := config.Config{Profile: "tiny"}
	normal := config.Config{Profile: "normal"}

	cases := []struct {
		name     string
		cfg      config.Config
		inIter   int
		inTok    int
		wantIter int
		wantTok  int
	}{
		// auto sentinels resolve by profile.
		{"tiny auto", tiny, 0, -1, 40, 400000},
		{"normal auto", normal, 0, -1, loop.MaxIterations, 0},
		// explicit values always win, even on tiny.
		{"tiny explicit iter", tiny, 7, -1, 7, 400000},
		{"explicit unlimited tokens", tiny, 0, 0, 40, 0},
		{"explicit both", normal, 12, 5000, 12, 5000},
	}
	for _, c := range cases {
		gotIter, gotTok := resolveZzzBudgets(c.cfg, c.inIter, c.inTok)
		if gotIter != c.wantIter || gotTok != c.wantTok {
			t.Errorf("%s: resolveZzzBudgets = (%d,%d) want (%d,%d)", c.name, gotIter, gotTok, c.wantIter, c.wantTok)
		}
	}
}
