// Package gitsafe builds host-side git commands that must not run code a
// sandboxed agent planted in the repo. An agent can write .git/hooks/* or set
// core.fsmonitor / core.hooksPath in the repo config; the next git call the
// host makes on that repo (commit, merge, status) would then execute it with
// the host's privileges, outside the sandbox.
package gitsafe

import "os/exec"

// flags neutralize the config-driven exec points: hooks and the fsmonitor
// daemon hook.
var flags = []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false"}

// Args prefixes args with the hardening flags.
func Args(args ...string) []string {
	return append(append([]string(nil), flags...), args...)
}

// Command is exec.Command("git", args...) with hooks and fsmonitor off.
func Command(args ...string) *exec.Cmd {
	return exec.Command("git", Args(args...)...)
}
