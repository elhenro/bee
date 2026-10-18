package sandbox

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// gitGuard lists repo paths under cwd that the sandbox must keep from the
// shell, because a later UNSANDBOXED git run by the host (zzz and agents commit
// from the host) would execute what was planted there.
type gitGuard struct {
	// ReadOnly: paths (files or trees) that run code on the next git command:
	// hooks, config (core.fsmonitor, core.hooksPath, aliases, filter drivers),
	// commondir (redirects hooks/config lookup to another gitdir), and the same
	// for submodule gitdirs under .git/modules.
	ReadOnly []string
	// Pinned: the `.git` entry itself. It must not be renamed or unlinked, or
	// a swapped-in gitfile/dir sends the host's git somewhere else.
	Pinned []string
	// ModuleRegex: seatbelt regexes for submodule hooks/config that may not
	// exist yet (the walk for ReadOnly only sees what is there now).
	ModuleRegex []string
}

// gitProtected computes the guard for cwd. Objects, refs and the index stay
// writable so commits inside the sandbox keep working. A linked worktree's
// `.git` is a file whose gitdir sits outside cwd (already read-only to the
// sandbox); only the pin applies there.
func gitProtected(cwd string) gitGuard {
	var g gitGuard
	for _, root := range cwdAliases(cwd) {
		dot := filepath.Join(root, ".git")
		fi, err := os.Lstat(dot)
		if err != nil {
			continue
		}
		g.Pinned = append(g.Pinned, dot)
		if !fi.IsDir() {
			continue
		}
		g.ReadOnly = append(g.ReadOnly,
			filepath.Join(dot, "hooks"),
			filepath.Join(dot, "config"),
			filepath.Join(dot, "config.lock"),
			filepath.Join(dot, "commondir"),
		)
		g.ReadOnly = append(g.ReadOnly, moduleGuards(filepath.Join(dot, "modules"))...)
		if !strings.ContainsAny(dot, `"\`) {
			g.ModuleRegex = append(g.ModuleRegex,
				"^"+regexQuote(filepath.Join(dot, "modules"))+"/.+/(hooks|config|config\\.lock|commondir)(/.*)?$")
		}
	}
	return g
}

// moduleGuards finds existing submodule hooks dirs and config files, a bounded
// walk since modules nest.
func moduleGuards(modules string) []string {
	var out []string
	base := strings.Count(modules, string(filepath.Separator))
	_ = filepath.WalkDir(modules, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if strings.Count(p, string(filepath.Separator))-base > 8 {
			return fs.SkipDir
		}
		switch d.Name() {
		case "hooks", "config", "config.lock", "commondir":
			out = append(out, p)
			if d.IsDir() {
				return fs.SkipDir
			}
		case "objects":
			return fs.SkipDir
		}
		return nil
	})
	return out
}

func regexQuote(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`\.+*?()|[]{}^$`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
