package tui

import (
	"context"
	"testing"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

type fakeShell struct {
	gotCmd string
	out    string
	isErr  bool
	err    error
}

func (f *fakeShell) Spec() llm.ToolSpec {
	return llm.ToolSpec{Name: "bash", Description: "fake"}
}
func (f *fakeShell) Run(ctx context.Context, in map[string]any) (tools.Result, error) {
	f.gotCmd, _ = in["command"].(string)
	return tools.Result{Content: f.out, IsError: f.isErr}, f.err
}

func TestParseInlinePrefix(t *testing.T) {
	cases := []struct {
		in     string
		cmd    string
		count  int
		inline bool
	}{
		{"!ls", "ls", 1, true},
		{"!!ls -la", "ls -la", 2, true},
		{"hello", "hello", 0, false},
		{"echo !", "echo !", 0, false},
	}
	for _, c := range cases {
		cmd, count, inline := parseInlinePrefix(c.in)
		if cmd != c.cmd || count != c.count || inline != c.inline {
			t.Errorf("parseInlinePrefix(%q) = (%q,%v,%v); want (%q,%v,%v)",
				c.in, cmd, count, inline, c.cmd, c.count, c.inline)
		}
	}
}

func TestResolveBangSilent(t *testing.T) {
	cases := []struct {
		defaultSilent bool
		count         int
		want          bool
	}{
		// default silent (new default): ! silent, !! forwards
		{true, 1, true},
		{true, 2, false},
		// default forwards (legacy): ! forwards, !! silent
		{false, 1, false},
		{false, 2, true},
	}
	for _, c := range cases {
		if got := resolveBangSilent(c.defaultSilent, c.count); got != c.want {
			t.Errorf("resolveBangSilent(default=%v, count=%d) = %v; want %v",
				c.defaultSilent, c.count, got, c.want)
		}
	}
}

func TestRunInlineShell_CallsShellTool(t *testing.T) {
	reg := tools.NewRegistry()
	fs := &fakeShell{out: "hello"}
	if err := reg.Register(fs); err != nil {
		t.Fatalf("register: %v", err)
	}
	res := runInlineShell(context.Background(), reg, "echo hi", false)
	if res.Output != "hello" {
		t.Errorf("got %q", res.Output)
	}
	if fs.gotCmd != "echo hi" {
		t.Errorf("shell called with %q", fs.gotCmd)
	}
}

func TestRunInlineShell_MissingShell(t *testing.T) {
	reg := tools.NewRegistry()
	res := runInlineShell(context.Background(), reg, "x", false)
	if res.Output != "no bash tool registered" {
		t.Errorf("got %q", res.Output)
	}
}

func TestRunInlineShell_EmptyCmd(t *testing.T) {
	reg := tools.NewRegistry()
	res := runInlineShell(context.Background(), reg, "  ", false)
	if res.Output != "" {
		t.Errorf("expected empty output, got %q", res.Output)
	}
}

func TestRunInlineShell_SurfacesIsError(t *testing.T) {
	reg := tools.NewRegistry()
	if err := reg.Register(&fakeShell{out: "exit 1", isErr: true}); err != nil {
		t.Fatalf("register: %v", err)
	}
	res := runInlineShell(context.Background(), reg, "false", false)
	if !res.IsError {
		t.Fatalf("expected IsError=true")
	}
	if res.Output != "exit 1" {
		t.Errorf("output: %q", res.Output)
	}
}
