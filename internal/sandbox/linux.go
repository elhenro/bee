package sandbox

import (
	"fmt"
	"strings"
)

// wrapLinux builds a bwrap invocation. Layout:
//   - / mounted read-only (--ro-bind / /)
//   - /proc and /dev synthesized
//   - tmpfs on /tmp
//   - cwd bound writable for WorkspaceWrite
//   - --unshare-net for ReadOnly (no network)
//   - --unshare-all minus net for WorkspaceWrite (network still off; matches
//     codex default — agents should not exfiltrate during writes)
//
// If bwrap is not on PATH the original cmd is returned with ErrHelperMissing.
func wrapLinux(p Policy, cmd []string) ([]string, error) {
	if _, err := lookPath("bwrap"); err != nil {
		return cmd, fmt.Errorf("%w: bwrap", ErrHelperMissing)
	}
	args, err := bwrapArgs(p)
	if err != nil {
		return cmd, err
	}
	wrapped := append([]string{"bwrap"}, args...)
	wrapped = append(wrapped, cmd...)
	return wrapped, nil
}

func bwrapArgs(p Policy) ([]string, error) {
	base := []string{
		"--ro-bind", "/", "/",
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		"--die-with-parent",
		"--new-session",
		"--unshare-pid",
		"--unshare-ipc",
		"--unshare-uts",
		"--unshare-cgroup-try",
	}
	switch p.Scope {
	case ReadOnly:
		base = append(base, "--unshare-net")
		return base, nil
	case WorkspaceWrite:
		cwd := strings.TrimSpace(p.Cwd)
		if cwd == "" {
			return nil, fmt.Errorf("sandbox: workspace-write requires Policy.Cwd")
		}
		base = append(base,
			"--unshare-net",
			"--bind", cwd, cwd,
			"--chdir", cwd,
		)
		return base, nil
	default:
		return nil, fmt.Errorf("sandbox: unsupported scope %q", p.Scope)
	}
}
