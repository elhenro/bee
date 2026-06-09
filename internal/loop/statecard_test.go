package loop

import (
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func TestStateCard_ObserveAndRender(t *testing.T) {
	c := newStateCard("fix the failing test")
	c.observe(
		[]types.ToolUse{
			{Name: "bash", Input: map[string]any{"command": "go test ./..."}},
			{Name: "read", Input: map[string]any{"path": "a.go"}},
		},
		[]types.ToolResult{
			{Content: "FAIL\tpkg/x 0.1s\nexit status 1", IsError: true},
			{Content: "package x ..."},
		},
	)
	c.observe(
		[]types.ToolUse{{Name: "edit_diff", Input: map[string]any{"path": "a.go"}}},
		[]types.ToolResult{{Content: "ok"}},
	)
	out := c.render()

	for _, want := range []string{
		"goal: fix the failing test",
		"a.go(edited)", // edit upgrades the earlier read verb
		"- bash(go test ./...) ERR: FAIL\tpkg/x 0.1s",
		"- read(a.go) ok",
		"last error: bash: FAIL\tpkg/x 0.1s",
		"progress: 3 tool calls over 2 turns",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("card missing %q:\n%s", want, out)
		}
	}
}

func TestStateCard_ActionsCapped(t *testing.T) {
	c := newStateCard("g")
	for i := 0; i < stateCardMaxActions+5; i++ {
		c.observe(
			[]types.ToolUse{{Name: "bash", Input: map[string]any{"command": "x"}}},
			[]types.ToolResult{{Content: "ok"}},
		)
	}
	if len(c.actions) != stateCardMaxActions {
		t.Fatalf("actions not capped: %d", len(c.actions))
	}
	if c.calls != stateCardMaxActions+5 {
		t.Fatalf("call counter wrong: %d", c.calls)
	}
}

func TestStateCard_ViewKeepsTailAndPairAlignment(t *testing.T) {
	mk := func(role types.Role) types.Message {
		return types.Message{Role: role, Content: []types.ContentBlock{{Type: types.BlockText, Text: "x"}}}
	}
	// long history ending so the naive tail cut would start on a RoleTool msg.
	msgs := []types.Message{
		mk(types.RoleUser), mk(types.RoleAssistant), mk(types.RoleTool),
		mk(types.RoleAssistant), mk(types.RoleTool),
		mk(types.RoleAssistant), mk(types.RoleTool),
		mk(types.RoleAssistant), mk(types.RoleTool),
		mk(types.RoleAssistant), mk(types.RoleTool),
	}
	c := newStateCard("g")
	out := c.view(msgs)

	if len(out) >= len(msgs) {
		t.Fatalf("view did not shrink history: %d -> %d", len(msgs), len(out))
	}
	if !strings.Contains(out[0].Content[0].Text, "[state card]") {
		t.Fatalf("first view message is not the card: %+v", out[0])
	}
	if out[1].Role == types.RoleTool {
		t.Fatalf("view starts with orphan tool_result")
	}
	// everything after the card must be the verbatim tail of msgs.
	tail := msgs[len(msgs)-(len(out)-1):]
	for i, m := range tail {
		if out[i+1].Role != m.Role {
			t.Fatalf("tail not verbatim at %d: %v vs %v", i, out[i+1].Role, m.Role)
		}
	}
}

func TestStateCard_ViewShortHistoryKeepsAll(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "do it"}}},
	}
	c := newStateCard("do it")
	out := c.view(msgs)
	if len(out) != 2 {
		t.Fatalf("short history view wrong size: %d", len(out))
	}
}
