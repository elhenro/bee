package types

import "testing"

func TestToolUse_Label(t *testing.T) {
	cases := []struct {
		name string
		use  ToolUse
		want string
	}{
		{"bash", ToolUse{Name: "bash", Input: map[string]any{"description": "  running tests \nmore"}}, "running tests"},
		{"missing", ToolUse{Name: "bash", Input: map[string]any{"command": "ls"}}, ""},
		{"wrong type", ToolUse{Name: "bash", Input: map[string]any{"description": 3}}, ""},
		{"unlabeled tool keeps its own arg", ToolUse{Name: "usertool", Input: map[string]any{"description": "mine"}}, ""},
	}
	for _, c := range cases {
		if got := c.use.Label(); got != c.want {
			t.Errorf("%s: Label() = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestToolUse_ArgsDropsLabelOnly(t *testing.T) {
	u := ToolUse{Name: "bash", Input: map[string]any{"command": "ls", "description": "listing"}}
	got := u.Args()
	if _, ok := got["description"]; ok || got["command"] != "ls" {
		t.Fatalf("Args() = %v, want command only", got)
	}
	if _, ok := u.Input["description"]; !ok {
		t.Fatal("Args() must not edit Input: the transcript keeps the label")
	}
	own := ToolUse{Name: "usertool", Input: map[string]any{"description": "mine"}}
	if own.Args()["description"] != "mine" {
		t.Fatal("a tool without labels keeps its own description arg")
	}
}

func TestToolUse_Why(t *testing.T) {
	edit := ToolUse{Name: "edit", Input: map[string]any{"path": "a.go", "why": "  guard nil  "}}
	if got := edit.Why(); got != "guard nil" {
		t.Fatalf("Why() = %q", got)
	}
	if (ToolUse{Name: "edit", Input: map[string]any{"path": "a.go"}}).Why() != "" {
		t.Fatal("a missing why is empty")
	}
	if (ToolUse{Name: "read", Input: map[string]any{"why": "mine"}}).Why() != "" {
		t.Fatal("a tool that takes no why keeps its own arg")
	}
}

func TestToolUse_ArgsDropsWhy(t *testing.T) {
	u := ToolUse{Name: "edit", Input: map[string]any{"path": "a.go", "why": "guard nil"}}
	got := u.Args()
	if _, ok := got["why"]; ok || got["path"] != "a.go" {
		t.Fatalf("Args() = %v, want path only", got)
	}
	if _, ok := u.Input["why"]; !ok {
		t.Fatal("Args() must not edit Input: the transcript keeps the why")
	}
	own := ToolUse{Name: "read", Input: map[string]any{"why": "mine"}}
	if own.Args()["why"] != "mine" {
		t.Fatal("a tool without a why keeps its own arg")
	}
}
