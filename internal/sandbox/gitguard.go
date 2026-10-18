package sandbox

import (
	"os"
	"path/filepath"
)

// gitProtected lists the repo paths under cwd that make the NEXT git command
// run arbitrary code: hooks, and config (core.fsmonitor, core.hooksPath,
// aliases, filter drivers). The workspace is writable, so without this a
// sandboxed shell can plant a hook that fires later in an unsandboxed git run
// by the host (zzz and agents commit from the host). Objects, refs and the
// index stay writable so commits inside the sandbox keep working. Only a real
// `.git` directory counts: a linked worktree's `.git` is a file and its gitdir
// sits outside cwd, already read-only to the sandbox.
func gitProtected(cwd string) []string {
	var out []string
	for _, root := range cwdAliases(cwd) {
		g := filepath.Join(root, ".git")
		if fi, err := os.Stat(g); err != nil || !fi.IsDir() {
			continue
		}
		out = append(out,
			filepath.Join(g, "hooks"),
			filepath.Join(g, "config"),
			filepath.Join(g, "config.lock"),
		)
	}
	return out
}
