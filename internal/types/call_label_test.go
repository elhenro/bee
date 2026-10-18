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
