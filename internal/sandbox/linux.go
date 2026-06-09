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
		return base, nil
	default:
		return nil, fmt.Errorf("sandbox: unsupported scope %q", p.Scope)
	}
}
