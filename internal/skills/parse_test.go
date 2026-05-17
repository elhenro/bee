package skills

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParse_AllKinds(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr bool
		check   func(t *testing.T, s Skill)
	}{
		{
			name: "prompt",
			body: `---
name: calc
type: prompt
description: stage and commit
tools: [shell, apply_patch]
auto_approve: ["shell:git"]
---
You are stage-then-commit.`,
			check: func(t *testing.T, s Skill) {
				if s.Kind != KindPrompt {
					t.Fatalf("kind: %s", s.Kind)
				}
				if s.Name != "calc" {
					t.Fatalf("meta: %+v", s)
				}
				if !strings.Contains(s.Body, "stage-then-commit") {
					t.Fatalf("body lost: %q", s.Body)
				}
				if len(s.Tools) != 2 {
					t.Fatalf("tools: %v", s.Tools)
				}
			},
		},
		{
			name: "exec",
			body: `---
name: hermes
type: exec
description: personal agent
exec: ["hermes", "--headless"]
stream: true
---
optional body`,
			check: func(t *testing.T, s Skill) {
				if s.Kind != KindExec {
					t.Fatalf("kind: %s", s.Kind)
				}
				if len(s.Exec) != 2 || s.Exec[0] != "hermes" {
					t.Fatalf("exec: %v", s.Exec)
				}
				if !s.Stream {
					t.Fatal("stream not set")
				}
			},
		},
		{
			name: "mcp",
			body: `---
name: slack
type: mcp
description: send slack messages
server:
  command: slack-mcp
  args: ["--token-env", "SLACK_TOKEN"]
  env: { FOO: bar }
---
`,
			check: func(t *testing.T, s Skill) {
				if s.Kind != KindMCP {
					t.Fatalf("kind: %s", s.Kind)
				}
				if s.Server.Command != "slack-mcp" {
					t.Fatalf("server: %+v", s.Server)
				}
				if s.Server.Env["FOO"] != "bar" {
					t.Fatalf("env: %v", s.Server.Env)
				}
			},
		},
		{
			name: "http",
			body: `---
name: notion
type: http
description: query notion
endpoint: https://example.local/q
auth:
  type: bearer
  env: NOTION_TOKEN
---
`,
			check: func(t *testing.T, s Skill) {
				if s.Kind != KindHTTP {
					t.Fatalf("kind: %s", s.Kind)
				}
				if s.Endpoint != "https://example.local/q" {
					t.Fatalf("endpoint: %s", s.Endpoint)
				}
				if s.Auth.Type != "bearer" || s.Auth.Env != "NOTION_TOKEN" {
					t.Fatalf("auth: %+v", s.Auth)
				}
			},
		},
		{
			name:    "invalid-no-frontmatter",
			body:    "just a markdown body, no yaml",
			wantErr: true,
		},
		{
			name: "invalid-unknown-type",
			body: `---
name: x
type: chimera
---
body`,
			wantErr: true,
		},
		{
			name: "invalid-missing-type",
			body: `---
name: x
description: no type
---
body`,
			wantErr: true,
		},
		{
			name: "invalid-exec-no-cmd",
			body: `---
name: x
type: exec
---
`,
			wantErr: true,
		},
		{
			name: "invalid-mcp-no-server",
			body: `---
name: x
type: mcp
---
`,
			wantErr: true,
		},
		{
			name: "invalid-http-no-endpoint",
			body: `---
name: x
type: http
---
`,
			wantErr: true,
		},
		{
			name: "invalid-prompt-empty-body",
			body: `---
name: x
type: prompt
---
`,
			wantErr: true,
		},
		{
			name: "invalid-name",
			body: `---
name: "bad name!"
type: prompt
---
body`,
			wantErr: true,
		},
		{
			name: "unterminated-frontmatter",
			body: `---
name: x
type: prompt
no closing delim`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Parse("test.md", []byte(tc.body))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want err, got skill %+v", s)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if tc.check != nil {
				tc.check(t, s)
			}
		})
	}
}

func TestParseFile_NameFallbackToFilename(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "release.md")
	body := `---
type: prompt
description: ship it
---
do the release dance`
	if err := writeFile(p, body); err != nil {
		t.Fatal(err)
	}
	s, err := ParseFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "release" {
		t.Fatalf("name fallback failed: %q", s.Name)
	}
}

func writeFile(p, body string) error {
	return writeFileBytes(p, []byte(body))
}
