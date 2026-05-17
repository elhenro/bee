package sandbox

import "testing"

func TestIsKnownSafe(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		// plain safe commands
		{"ls", true},
		{"ls -la", true},
		{"pwd", true},
		{"cat README.md", true},
		{"grep -rn foo .", true},
		{"find . -name '*.go'", true},
		// multi-word prefixes
		{"git status", true},
		{"git status --short", true},
		{"git diff HEAD~1", true},
		{"go build ./...", true},
		{"go test -run TestX ./internal/sandbox", true},
		// not-safe
		{"git stash", false},
		{"git push", false},
		{"rm -rf /", false},
		{"curl https://evil.example", false},
		{"go run ./cmd/bee", false},
		// word-boundary discipline
		{"github", false},
		{"goat", false},
		{"lsattr", false},
		// empties
		{"", false},
		{"   ", false},
	}
	for _, tc := range cases {
		got := IsKnownSafe(tc.cmd)
		if got != tc.want {
			t.Errorf("IsKnownSafe(%q) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

func TestPolicyIsAllowed_AllowList(t *testing.T) {
	p := Policy{AllowList: []string{"npm test", "make"}}
	cases := map[string]bool{
		"ls":            true,  // built-in safe
		"npm test":      true,  // custom
		"npm test -- a": true,  // custom prefix
		"npm install":   false, // not on list
		"make build":    true,
		"makefile":      false, // word boundary
	}
	for cmd, want := range cases {
		if got := p.IsAllowed(cmd); got != want {
			t.Errorf("Policy.IsAllowed(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestScopeAndApprovalConsts(t *testing.T) {
	// guard against accidental rename — these strings are part of the
	// config-file surface area and must remain stable.
	if string(ReadOnly) != "read-only" {
		t.Errorf("ReadOnly = %q", ReadOnly)
	}
	if string(WorkspaceWrite) != "workspace-write" {
		t.Errorf("WorkspaceWrite = %q", WorkspaceWrite)
	}
	if string(DangerFullAccess) != "danger-full-access" {
		t.Errorf("DangerFullAccess = %q", DangerFullAccess)
	}
	if string(OnRequest) != "on-request" {
		t.Errorf("OnRequest = %q", OnRequest)
	}
}
