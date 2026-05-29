package main

import (
	"context"
	"errors"
	"testing"

	"github.com/elhenro/bee/internal/loop"
)

// wedge sentinels must be recoverable; escalate + arbitrary errors must not be,
// so the headless goal loop only swallows transient generation stalls and still
// hard-exits on real failures or a deliberate user-escalation.
func TestIsWedgedTurn(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"two-strike", &loop.TwoStrikeError{}, true},
		{"per-tool", &loop.PerToolFailureError{}, true},
		{"format-strike", &loop.FormatStrikeError{}, true},
		{"repeat-stream", &loop.RepeatStreamError{}, true},
		{"escalate", &loop.EscalateError{Reason: "stuck"}, false},
		{"context-canceled", context.Canceled, false},
		{"arbitrary", errors.New("provider down"), false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := isWedgedTurn(c.err); got != c.want {
			t.Errorf("%s: isWedgedTurn = %v, want %v", c.name, got, c.want)
		}
	}
}
