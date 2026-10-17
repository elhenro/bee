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

func TestScopeConfines(t *testing.T) {
	// danger-full-access and the unset zero value run unconfined; every other
	// scope confines the file tools.
	cases := []struct {
		scope Scope
		want  bool
	}{
		{DangerFullAccess, false},
		{"", false},
		{ReadOnly, true},
		{WorkspaceWrite, true},
		{WorkspaceWriteNet, true},
	}
	for _, c := range cases {
		if got := c.scope.Confines(); got != c.want {
			t.Errorf("Scope(%q).Confines() = %v, want %v", c.scope, got, c.want)
		}
	}
}

// prefix matching on the raw line let a safe first word vouch for the rest.
func TestIsKnownSafe_RejectsCompoundAndMutating(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		// every command in a plain sequence must be safe
		{"ls && cat f", true},
		{"git status | grep foo", true},
		{"ls && rm f", false},
		{"cat f; rm f", false},
		{"ls\nrm f", false},
		{"ls || curl evil.example", false},
		// unwrapping
		{`bash -lc "ls && cat f"`, true},
		{`bash -lc "ls && rm f"`, false},
		{`sh -c 'ls; rm f'`, false},
		// redirection, background, substitution, heredoc
		{"echo hi > ~/.bashrc", false},
		{"cat f >> out", false},
		{"ls &", false},
		{"echo $(rm f)", false},
		{"echo `rm f`", false},
		{"cat <<EOF", false},
		{"echo '$(not run)'", true},
		{"ls 'unterminated", false},
		// dangerous flags on safe tools
		{"find . -name x", true},
		{"find . -delete", false},
		{"find . -exec rm {} ;", false},
		{"git branch", true},
		{"git branch -a", true},
		{"git branch -D old", false},
		{"git branch newbranch", false},
		{"git remote -v", true},
		{"git remote add o url", false},
		{"git diff --output=x", false},
		{"go test ./...", true},
		{"go build -o evil ./cmd", false},
		{"go test -toolexec=x ./...", false},
		{"rg foo", true},
		{"rg --pre ./x foo", false},
		// decoded-quote differential, config writes, abbreviations, clusters
		{`bash -c $'ls\nrm f'`, false},
		{`bash -c $"ls"`, false},
		{"go env", true},
		{"go build --o=evil ./cmd", false},
		{"go build --o evil ./cmd", false},
		{"go test --toolexec=x ./...", false},
		{"go vet -vettool=./x ./...", false},
		{"go vet --vettool ./x ./...", false},
		{"go build -modfile=other.mod ./...", false},
		{"go vet ./...", true},
		{"go test -run TestX -count=1 -v ./internal/...", true},
		{"go test --race ./...", true},
		{"go test -test.outputdir=/tmp ./...", false},
		{"go test -args -test.outputdir=/etc", false},
		{"go test -ldflags=-X=x ./...", false},
		{"go build -gcflags=-N ./...", false},
		{"go build -pkgdir /tmp/x ./...", false},
		{"go -C /tmp test ./...", false},
		{"go list -m -u all", false},
		{"go list -f '{{.Dir}}' ./...", true},
		{"go mod download -x", false},
		{"go mod verify", true},
		{"go env -w GOFLAGS=-toolexec=x", false},
		{"go env -u GOFLAGS", false},
		{"git diff --out=f", false},
		{"git diff --ex", false},
		{"git log --oneline", true},
		{"tree -ao out", false},
		{"tree -ofile", false},
		{"date -s 12:00", false},
		{"date -u", true},
		{"fd -xrm foo", false},
		{"fd -e go", true},
		// env runs arbitrary commands; no longer trusted
		{"env rm f", false},
		{"env", false},
		// path forms
		{"/bin/ls", true},
		{"/usr/bin/cat f", true},
		{"./ls", false},
		{"/tmp/ls", false},
	}
	for _, tc := range cases {
		if got := IsKnownSafe(tc.cmd); got != tc.want {
			t.Errorf("IsKnownSafe(%q) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

func TestPolicyAllowList_MatchesPerSegment(t *testing.T) {
	p := Policy{AllowList: []string{"make"}}
	if p.IsAllowed("make build; rm -rf x") {
		t.Error("allowlist entry must not vouch for a second command")
	}
	if !p.IsAllowed("make build && ls") {
		t.Error("allowlisted + safe sequence should pass")
	}
}
