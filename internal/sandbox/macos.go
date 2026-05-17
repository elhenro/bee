package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// seatbeltDevNodes is the always-allowed write set for stdio + dev nodes that
// every UNIX-y program touches (git writes to /dev/null, terminal apps open
// /dev/tty, DTrace stub is consulted at exec). Without these every shelled
// `git` returns exit 128 with "fatal: could not open '/dev/null'...".
const seatbeltDevNodes = `(allow file-write*
    (literal "/dev/null")
    (literal "/dev/zero")
    (literal "/dev/random")
    (literal "/dev/urandom")
    (literal "/dev/tty")
    (literal "/dev/dtracehelper")
    (literal "/dev/stdin")
    (literal "/dev/stdout")
    (literal "/dev/stderr"))
`

// seatbeltReadOnly denies all real file writes and network but still allows
// the dev/stdio nodes so subprocesses (notably git) can run.
const seatbeltReadOnly = `(version 1)
(deny default)
(allow process-exec)
(allow process-fork)
(allow signal (target self))
(allow file-read*)
(allow sysctl-read)
(allow mach-lookup)
(deny network*)
(deny file-write*)
` + seatbeltDevNodes

// seatbeltWorkspaceWriteHead/Tail wrap a dynamic block of (subpath ...) rules.
// The middle is built per-call so dev-tool caches under $HOME (go-build, go
// mod cache, npm, cargo, pip, etc.) can be written without dropping out of
// the sandbox. Without these, `go build` inside the wrapped shell tool fails
// with "open …/Library/Caches/go-build/…" because seatbelt blocks the write.
const seatbeltWorkspaceWriteHead = `(version 1)
(deny default)
(allow process-exec)
(allow process-fork)
(allow signal (target self))
(allow file-read*)
(allow sysctl-read)
(allow mach-lookup)
(deny network*)
(deny file-write*)
` + seatbeltDevNodes + `(allow file-write*
    (subpath "/private/tmp")
    (subpath "/private/var/folders")
    (subpath "/tmp")`
const seatbeltWorkspaceWriteTail = `)
`

// wrapMacOS prepends a `sandbox-exec -p <profile>` invocation. On macOS
// sandbox-exec ships with the OS, so we don't probe PATH — but we still
// return a warning if the policy needs cwd and none is set.
func wrapMacOS(p Policy, cmd []string) ([]string, error) {
	profile, err := macosProfile(p)
	if err != nil {
		return cmd, err
	}
	if _, err := lookPath("sandbox-exec"); err != nil {
		return cmd, fmt.Errorf("%w: sandbox-exec", ErrHelperMissing)
	}
	wrapped := []string{"sandbox-exec", "-p", profile}
	wrapped = append(wrapped, cmd...)
	return wrapped, nil
}

func macosProfile(p Policy) (string, error) {
	switch p.Scope {
	case ReadOnly:
		return seatbeltReadOnly, nil
	case WorkspaceWrite:
		cwd := strings.TrimSpace(p.Cwd)
		if cwd == "" {
			return "", fmt.Errorf("sandbox: workspace-write requires Policy.Cwd")
		}
		var b strings.Builder
		b.WriteString(seatbeltWorkspaceWriteHead)
		for _, path := range cwdAliases(cwd) {
			b.WriteString("\n    ")
			b.WriteString(fmt.Sprintf("(subpath %q)", path))
		}
		for _, d := range devCacheDirs() {
			b.WriteString("\n    ")
			b.WriteString(fmt.Sprintf("(subpath %q)", d))
		}
		b.WriteString(seatbeltWorkspaceWriteTail)
		return b.String(), nil
	default:
		return "", fmt.Errorf("sandbox: unsupported scope %q", p.Scope)
	}
}

// cwdAliases returns the cwd plus any canonical-resolved aliases. seatbelt
// canonicalizes the operand path before matching subpath, so when the user
// runs bee from a firmlinked or symlinked path (e.g. ~/web/bee that maps to
// the same inode as ~/projects-new/bee), writes resolve to the canonical
// path and miss a subpath built from the literal cwd — giving EPERM
// "Operation not permitted" on anything under .git. Include both forms.
//
// EvalSymlinks handles POSIX symlinks but not APFS firmlinks (Readlink
// reports ENOENT/EINVAL on those), so we also shell out to realpath(3) via
// /usr/bin/realpath which follows firmlinks. dedupes preserving order.
func cwdAliases(cwd string) []string {
	seen := map[string]bool{cwd: true}
	out := []string{cwd}
	if p, err := filepath.EvalSymlinks(cwd); err == nil && !seen[p] {
		seen[p] = true
		out = append(out, p)
	}
	if data, err := exec.Command("/usr/bin/realpath", cwd).Output(); err == nil {
		p := strings.TrimRight(string(data), "\n")
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// devCacheDirs returns the per-user dev-tool cache locations the workspace
// profile must writable so build/test commands don't fail under sandbox-exec.
// Honours $GOCACHE / $GOMODCACHE when set; otherwise uses defaults. Missing
// $HOME returns an empty slice — sandbox still works, just without these.
func devCacheDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	dirs := []string{
		filepath.Join(home, "Library", "Caches", "go-build"),
		filepath.Join(home, "go", "pkg", "mod"),
		filepath.Join(home, ".cache"),
		filepath.Join(home, ".npm"),
		filepath.Join(home, ".cargo", "registry"),
		filepath.Join(home, ".bee"),
	}
	if gc := strings.TrimSpace(os.Getenv("GOCACHE")); gc != "" {
		dirs = append(dirs, gc)
	}
	if gm := strings.TrimSpace(os.Getenv("GOMODCACHE")); gm != "" {
		dirs = append(dirs, gm)
	}
	return dirs
}
