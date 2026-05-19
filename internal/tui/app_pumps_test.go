package tui

import (
	"testing"
	"time"

	"github.com/elhenro/bee/internal/types"
)

func TestRecapWorthwhile(t *testing.T) {
	asst := func(text string) types.Message {
		return types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: text}},
		}
	}
	user := func(text string) types.Message {
		return types.Message{
			Role:    types.RoleUser,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: text}},
		}
	}
	tool := types.Message{
		Role: types.RoleAssistant,
		Content: []types.ContentBlock{
			{Type: types.BlockToolUse, Use: &types.ToolUse{Name: "read"}},
		},
	}

	cases := []struct {
		name string
		msgs []types.Message
		dur  time.Duration
		want bool
	}{
		{
			name: "short_greeting_skips",
			msgs: []types.Message{user("hi"), asst("Hey!")},
			dur:  2 * time.Second,
			want: false,
		},
		{
			name: "long_duration_triggers",
			msgs: []types.Message{user("hi"), asst("Hey!")},
			dur:  20 * time.Second,
			want: true,
		},
		{
			name: "tool_use_triggers",
			msgs: []types.Message{user("read foo"), tool, asst("Done.")},
			dur:  1 * time.Second,
			want: true,
		},
		{
			name: "long_reply_triggers",
			msgs: []types.Message{user("explain"), asst(repeat("x", 700))},
			dur:  3 * time.Second,
			want: true,
		},
		{
			name: "stops_at_previous_user",
			msgs: []types.Message{
				asst(repeat("y", 1000)),
				user("new"),
				asst("short"),
			},
			dur:  1 * time.Second,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := recapWorthwhile(tc.msgs, tc.dur)
			if got != tc.want {
				t.Fatalf("recapWorthwhile = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRecapIdleTickDropsOnGenMismatch(t *testing.T) {
	m := Model{showRecap: true, state: StateIdle, recapGen: 7}
	out, cmd := m.onRecapIdleTick(recapIdleTickMsg{gen: 6, msgs: nil})
	if cmd != nil {
		t.Fatalf("stale gen tick should drop, got cmd=%v", cmd)
	}
	if got := out.(Model).recapGen; got != 7 {
		t.Fatalf("recapGen mutated: got %d want 7", got)
	}
}

func TestRecapIdleTickDropsWhenNotIdle(t *testing.T) {
	m := Model{showRecap: true, state: StateStreaming, recapGen: 3}
	_, cmd := m.onRecapIdleTick(recapIdleTickMsg{gen: 3, msgs: nil})
	if cmd != nil {
		t.Fatalf("non-idle tick should drop, got cmd=%v", cmd)
	}
}

func TestRecapIdleTickDropsWhenDisabled(t *testing.T) {
	m := Model{showRecap: false, state: StateIdle, recapGen: 3}
	_, cmd := m.onRecapIdleTick(recapIdleTickMsg{gen: 3, msgs: nil})
	if cmd != nil {
		t.Fatalf("disabled tick should drop, got cmd=%v", cmd)
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
