package prompt

import (
	"os"
	"os/exec"
	"strings"
)

// getCwd returns the process cwd, or "?" if unavailable. Used by the
// identity block.
func getCwd() string {
	if d, err := os.Getwd(); err == nil {
		return d
	}
	return "?"
}

// gitRootFor returns the git toplevel for dir, or "" if dir is not in a
// git repo / git is unavailable. Cheap (cached internally per process
// would be nicer but assemble runs once per turn).
func gitRootFor(dir string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
