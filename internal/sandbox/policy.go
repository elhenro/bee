// Package sandbox implements the two-axis sandbox model (Scope x ApprovalMode).
// Scope is the OS-level confinement; ApprovalMode
// is the agent-loop policy for when to ask the user.
//
// The sandbox is best-effort hardening, not a security boundary. If the host
// lacks the required helper (sandbox-exec on macOS, bwrap on Linux) Wrap
// degrades to running the bare command and emitting a warning.
package sandbox

import (
	"strings"

	"github.com/elhenro/bee/internal/safety"
)

// Scope is the OS-level confinement applied to a child process.
type Scope string

const (
	// ReadOnly: filesystem read-only, network blocked.
	ReadOnly Scope = "read-only"
	// WorkspaceWrite: writes confined to cwd (and tmp), network blocked.
	WorkspaceWrite Scope = "workspace-write"
	// WorkspaceWriteNet: like WorkspaceWrite but outbound network is allowed, so
	// package installs (npm/pip/go/cargo) work while writes stay confined to
	// cwd+tmp. The default — network isn't the security boundary (the approval
	// modal is), filesystem confinement is what this preserves.
	WorkspaceWriteNet Scope = "workspace-write-net"
	// DangerFullAccess: no sandbox applied.
	DangerFullAccess Scope = "danger-full-access"
)

// Confines reports whether this scope restricts the file tools to the
// workspace and blocks secret-path reads. Only danger-full-access (and the
// empty/unset zero value) run unconfined — bee's default "do anything"
// posture, where file tools accept any path. Every other scope confines, to
// match the OS sandbox already applied to shell commands under that scope.
func (s Scope) Confines() bool {
	switch s {
	case "", DangerFullAccess:
		return false
	default:
		return true
	}
}

// known reports whether s names a scope with a defined policy.
func (s Scope) known() bool {
	switch s {
	case ReadOnly, WorkspaceWrite, WorkspaceWriteNet, DangerFullAccess:
		return true
	}
	return false
}

// ApprovalMode is the agent-loop policy for escalation prompts.
type ApprovalMode string

const (
	// Untrusted: auto-run only known-safe commands; ask for everything else.
	Untrusted ApprovalMode = "untrusted"
	// OnRequest: model decides when to ask via request_permissions (default).
	OnRequest ApprovalMode = "on-request"
	// OnFailure: run optimistically; ask only when sandbox blocks.
	OnFailure ApprovalMode = "on-failure"
	// Never: fully autonomous; never ask.
	Never ApprovalMode = "never"
)

// Policy is the resolved sandbox configuration for one tool invocation.
type Policy struct {
	Scope     Scope
	Approval  ApprovalMode
	Cwd       string
	AllowList []string // extra known-safe prefixes for Untrusted mode
}

// defaultSafe is the built-in allowlist of read-only / inspection commands
// that Untrusted mode may auto-run without asking. Prefix match: an entry "git
// status" matches "git status" and "git status --short" but not "git stash".
var defaultSafe = []string{
	// inspection
	"ls", "pwd", "cat", "head", "tail", "wc", "file", "echo", "true", "false", "which", "whoami",
	"hostname", "uname", "date", "printenv", "stat", "du", "df",
	// search
	"grep", "egrep", "fgrep", "rg", "find", "fd", "tree",
	// git
	"git status", "git diff", "git log", "git show", "git branch", "git remote", "git rev-parse",
	"git config --get",
	// go
	"go build", "go test", "go vet", "go list", "go env", "go version", "go mod download", "go mod verify",
}

// IsAllowed reports whether every command in cmd matches a built-in safe
// prefix or the policy's per-invocation AllowList. cmd is tokenized first: a
// compound line is safe only when each simple command is, and redirections,
// substitutions and flags that turn a read-only tool into a writer (find
// -exec, git branch -D, ...) are refused. The list is matched per simple
// command, never against the raw line, so "ls; rm x" cannot ride on an "ls"
// entry.
func (p Policy) IsAllowed(cmd string) bool {
	segs, ok := safety.PlainSegments(cmd)
	if !ok {
		return false
	}
	for _, argv := range segs {
		if !p.segmentAllowed(argv) {
			return false
		}
	}
	return true
}

func (p Policy) segmentAllowed(argv []string) bool {
	if safety.UnsafeInvocation(argv) {
		return false
	}
	argv = append([]string{safety.CommandName(argv[0])}, argv[1:]...)
	line := strings.Join(argv, " ")
	for _, prefix := range defaultSafe {
		if hasCmdPrefix(line, prefix) {
			return true
		}
	}
	for _, prefix := range p.AllowList {
		if hasCmdPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// hasCmdPrefix matches at word boundaries: "git" matches "git status" but
// not "github". Prefix must equal cmd or be followed by whitespace.
func hasCmdPrefix(cmd, prefix string) bool {
	if cmd == prefix {
		return true
	}
	if !strings.HasPrefix(cmd, prefix) {
		return false
	}
	next := cmd[len(prefix)]
	return next == ' ' || next == '\t'
}
