package sandbox

import "fmt"

// wrapWindows is a stub. WSL2 dispatch is post-v0.1 — for now we degrade to
// the bare command with a warning so the agent loop can decide whether to
// proceed or abort based on its ApprovalMode.
//
// TODO(post-v0.1): detect WSL2, re-dispatch the command via `wsl.exe -e ...`
// using the Linux bwrap path inside the distro.
func wrapWindows(p Policy, cmd []string) ([]string, error) {
	return cmd, fmt.Errorf("%w: windows sandbox not implemented (WSL2 dispatch post-v0.1)", ErrUnsupported)
}
