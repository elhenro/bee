package waggle

import "testing"

// noParams renders every arg as a literal.
func noParams(int, string) string { return "" }

func TestShellCommand_GrepLiteral(t *testing.T) {
	c := Call{Tool: "grep", Args: map[string]string{"pattern": "foo", "path": "src"}}
	got, ok := shellCommand(0, c, noParams)
	if !ok || got != "grep -rn 'foo' 'src'" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}

func TestShellCommand_GrepParam(t *testing.T) {
	c := Call{Tool: "grep", Args: map[string]string{"pattern": "foo"}}
	tok := func(step int, key string) string {
		if key == "pattern" {
			return "$1"
		}
		return ""
	}
	got, ok := shellCommand(0, c, tok)
	if !ok || got != `grep -rn "$1" .` {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}

func TestShellCommand_Read(t *testing.T) {
	got, ok := shellCommand(0, Call{Tool: "read", Args: map[string]string{"path": "a.go"}}, noParams)
	if !ok || got != "cat 'a.go'" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}

func TestShellCommand_LsDefaultDot(t *testing.T) {
	got, ok := shellCommand(0, Call{Tool: "ls", Args: map[string]string{}}, noParams)
	if !ok || got != "ls ." {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}

func TestShellCommand_Find(t *testing.T) {
	c := Call{Tool: "find", Args: map[string]string{"pattern": "*.go", "path": "src"}}
	got, ok := shellCommand(0, c, noParams)
	if !ok || got != "find 'src' -name '*.go'" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}

func TestShellCommand_BashVerbatim(t *testing.T) {
	got, ok := shellCommand(0, Call{Tool: "bash", Args: map[string]string{"command": "echo hi"}}, noParams)
	if !ok || got != "echo hi" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}

func TestShellCommand_BashParamNotSupported(t *testing.T) {
	tok := func(int, string) string { return "$1" }
	if _, ok := shellCommand(0, Call{Tool: "bash", Args: map[string]string{"command": "x"}}, tok); ok {
		t.Fatal("parameterized raw bash must not be crystallizable")
	}
}

func TestShellCommand_UnknownTool(t *testing.T) {
	if _, ok := shellCommand(0, Call{Tool: "browser", Args: map[string]string{}}, noParams); ok {
		t.Fatal("unknown tool must not translate")
	}
}
