// Package sandbox implements the two-axis sandbox model (Scope x ApprovalMode).
// Scope is the OS-level confinement; ApprovalMode
// is the agent-loop policy for when to ask the user.
//
// The sandbox is best-effort hardening, not a security boundary. If the host
// lacks the required helper (sandbox-exec on macOS, bwrap on Linux) Wrap
// degrades to running the bare command and emitting a warning.
package sandbox

import "strings"

// Scope is the OS-level confinement applied to a child process.
type Scope string

const (
	// ReadOnly: filesystem read-only, network blocked.
	ReadOnly Scope = "read-only"
	// WorkspaceWrite: writes confined to cwd (and tmp), network blocked.
	WorkspaceWrite Scope = "workspace-write"
	// DangerFullAccess: no sandbox applied.
	DangerFullAccess Scope = "danger-full-access"
)

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
	"ls",
	"pwd",
	"cat",
	"head",
	"tail",
	"wc",
	"file",
	"echo",
	"true",
	"false",
	"which",
	"whoami",
	"hostname",
	"uname",
	"date",
	"env",
	"printenv",
	"grep",
	"egrep",
	"fgrep",
	"rg",
	"find",
	"fd",
	"tree",
	"stat",
	"du",
	"df",
	"git status",
	"git diff",
	"git log",
	"git show",
	"git branch",
	"git remote",
	"git rev-parse",
	"git config --get",
	"go build",
	"go test",
	"go vet",
	"go list",
	"go env",
	"go version",
	"go mod download",
	"go mod verify",
}

// IsKnownSafe reports whether cmd matches a built-in safe prefix. Used by
// Untrusted mode to auto-approve without escalation.
func IsKnownSafe(cmd string) bool {
	c := strings.TrimSpace(cmd)
	if c == "" {
		return false
	}
	for _, prefix := range defaultSafe {
		if hasCmdPrefix(c, prefix) {
			return true
		}
	}
	return false
}

// IsAllowed extends IsKnownSafe with the policy's per-invocation AllowList.
func (p Policy) IsAllowed(cmd string) bool {
	if IsKnownSafe(cmd) {
		return true
	}
	c := strings.TrimSpace(cmd)
	for _, prefix := range p.AllowList {
		if hasCmdPrefix(c, prefix) {
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
