package sandbox

import (
	"errors"
	"testing"
)

// a policy that cannot be applied must fail closed even where bwrap is absent:
// ErrHelperMissing degrades to the bare command, so it may only surface once
// the policy itself is valid.
func TestWrapLinux_InvalidPolicyBeatsMissingHelper(t *testing.T) {
	withLookPath(t, func(string) (string, error) { return "", errors.New("not found") })
	cmd := []string{"ls"}

	_, err := wrapLinux(Policy{Scope: WorkspaceWrite}, cmd)
	if err == nil || errors.Is(err, ErrHelperMissing) {
		t.Fatalf("missing cwd: err = %v, want a policy error", err)
	}

	_, err = wrapLinux(Policy{Scope: WorkspaceWrite, Cwd: "/tmp"}, cmd)
	if !errors.Is(err, ErrHelperMissing) {
		t.Fatalf("valid policy, no bwrap: err = %v, want ErrHelperMissing", err)
	}
}
