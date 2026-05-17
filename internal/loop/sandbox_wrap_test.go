package loop

import (
	"testing"

	"github.com/elhenro/bee/internal/config"
)

func TestJoinShell(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want string
	}{
		{
			name: "simple",
			argv: []string{"/bin/bash", "-c", "echo hi"},
			want: "/bin/bash -c 'echo hi'",
		},
		{
			name: "empty arg",
			argv: []string{"/bin/bash", "", "arg"},
			want: "/bin/bash '' arg",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := joinShell(tc.argv)
			if got != tc.want {
				t.Errorf("joinShell(%v) = %q, want %q", tc.argv, got, tc.want)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	q := string('\'')
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"alphanumeric", "hello123", "hello123"},
		{"path", "/var/log/app.log", "/var/log/app.log"},
		{"dashes", "my-file_name.v2", "my-file_name.v2"},
		{"spaces", "hello world", "'hello world'"},
		{"single quote", "it's a test", q + "it" + q + `\'` + q + "s a test" + q},
		{"semicolon", "echo a; rm -rf /", "'echo a; rm -rf /'"},
		{"pipe", "cat file | grep foo", "'cat file | grep foo'"},
		{"redirect", "cat > output.txt", "'cat > output.txt'"},
		{"dollar", "$HOME/path", "'$HOME/path'"},
		{"backtick", "echo `ls`", "'echo `ls`'"},
		{"ampersand", "cmd & jobs", "'cmd & jobs'"},
		{"equals", "key=value", "key=value"},
		{"empty", "", "''"},
		{"unicode", "üñíçödé", "'üñíçödé'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shellQuote(tc.input)
			if got != tc.want {
				t.Errorf("shellQuote(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestWrapShellInput_NoCommand(t *testing.T) {
	input := map[string]any{"other": "value"}
	cfg := config.SandboxConfig{Scope: "workspace-write", Approval: "on-request"}
	got := wrapShellInput(input, cfg, "/tmp")
	if got["command"] != nil {
		t.Errorf("no command input: got command=%v, want nil", got["command"])
	}
}

func TestWrapShellInput_EmptyCommand(t *testing.T) {
	input := map[string]any{"command": ""}
	cfg := config.SandboxConfig{Scope: "workspace-write", Approval: "on-request"}
	got := wrapShellInput(input, cfg, "/tmp")
	// empty command short-circuits — input passes through unwrapped
	if got["command"] != "" {
		t.Errorf("empty command: got command=%v, want unchanged empty", got["command"])
	}
}

func TestWrapShellInput_DangerFullAccess(t *testing.T) {
	input := map[string]any{"command": "echo hi"}
	cfg := config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
	got := wrapShellInput(input, cfg, "/tmp")
	if got["command"] != "echo hi" {
		t.Errorf("danger-full-access: command=%v, want unchanged", got["command"])
	}
}

func TestWrapShellInput_EmptyScope(t *testing.T) {
	input := map[string]any{"command": "echo hi"}
	cfg := config.SandboxConfig{}
	got := wrapShellInput(input, cfg, "/tmp")
	if got["command"] != "echo hi" {
		t.Errorf("empty scope: command=%v, want unchanged", got["command"])
	}
}

func TestWrapShellInput_WorkspaceWrite(t *testing.T) {
	input := map[string]any{"command": "echo hi"}
	cfg := config.SandboxConfig{Scope: "workspace-write", Approval: "on-request"}
	got := wrapShellInput(input, cfg, "/home/user/project")
	cmd, ok := got["command"].(string)
	if !ok || cmd == "" {
		t.Fatalf("workspace-write: command=%v, want wrapped", got["command"])
	}
	if len(cmd) <= 3 {
		t.Errorf("wrapped command too short (%d chars): %q", len(cmd), cmd)
	}
}

func TestWrapShellInput_PreservesExtraKeys(t *testing.T) {
	input := map[string]any{"command": "echo hi", "extra": "data", "count": 42}
	cfg := config.SandboxConfig{Scope: "workspace-write", Approval: "on-request"}
	got := wrapShellInput(input, cfg, "/tmp")
	if got["extra"] != "data" {
		t.Errorf("extra key lost: %v", got)
	}
	if got["count"] != 42 {
		t.Errorf("numeric extra key lost: %v", got)
	}
}
