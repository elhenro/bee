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
//   - --unshare-net for ReadOnly + WorkspaceWrite (no network)
//   - WorkspaceWriteNet keeps the host net namespace (outbound allowed for
//     package installs); writes stay confined to the bound cwd
//
// If bwrap is not on PATH the original cmd is returned with ErrHelperMissing.
func wrapLinux(p Policy, cmd []string) ([]string, error) {
	// validate the policy before probing for the helper: an unappliable scope
	// (missing cwd) must fail closed even where bwrap is absent, not degrade.
	args, err := bwrapArgs(p)
	if err != nil {
		return cmd, err
	}
	if _, err := lookPath("bwrap"); err != nil {
		return cmd, fmt.Errorf("%w: bwrap", ErrHelperMissing)
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
	case WorkspaceWrite, WorkspaceWriteNet:
		cwd := strings.TrimSpace(p.Cwd)
		if cwd == "" {
			return nil, fmt.Errorf("sandbox: %s requires Policy.Cwd", p.Scope)
		}
		// workspace-write-net keeps the host net namespace (outbound network
		// allowed for installs); plain workspace-write unshares it.
		if p.Scope == WorkspaceWrite {
			base = append(base, "--unshare-net")
		}
		base = append(base,
			"--bind", cwd, cwd,
			"--chdir", cwd,
		)
		// dev-tool caches writable, mirroring the macOS profile: go/npm/cargo
		// builds write to ~/.cache, ~/go, etc. and fail with EPERM under the
		// read-only root otherwise. bind-try skips dirs that don't exist.
		for _, d := range devCacheDirs() {
			base = append(base, "--bind-try", d, d)
		}
		// last, so these mounts sit on top of every writable bind above.
		// pinning `.git` as its own mount makes rename/unlink of it fail with
		// EBUSY; the read-only mounts cover hooks, config and friends.
		g := gitProtected(cwd)
		for _, path := range g.Pinned {
			base = append(base, "--bind-try", path, path)
		}
		for _, path := range g.ReadOnly {
			base = append(base, "--ro-bind-try", path, path)
		}
		return base, nil
	default:
		return nil, fmt.Errorf("sandbox: unsupported scope %q", p.Scope)
	}
}
