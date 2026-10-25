package sandbox

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
)

// ErrUnsupported signals that wrapping is not implemented for this platform.
// Wrap returns this alongside the unwrapped command — the caller decides
// whether to abort or proceed with degraded confinement.
var ErrUnsupported = errors.New("sandbox: platform not supported")

// ErrHelperMissing signals that the OS-level helper (bwrap, sandbox-exec) is
// not on PATH. The original cmd is returned so the caller can degrade.
var ErrHelperMissing = errors.New("sandbox: helper binary not found")

// ErrUnknownScope signals a scope string that names no policy. Scope.Confines
// treats it as confined (file tools refuse paths), so Wrap must not let the
// shell run bare: it returns a command that refuses instead of cmd.
var ErrUnknownScope = errors.New("sandbox: unknown scope")

// RefuseCmd is a 5-element argv (callers treat 3 or fewer as "nothing wrapped")
// that prints reason and exits 126 without running the model's command. Callers
// use it to fail closed when a confining scope cannot be applied.
func RefuseCmd(reason string) []string {
	return []string{"sh", "-c", `printf '%s\n' "$1" >&2; exit 126`, "bee-sandbox", reason}
}

func unknownScopeCmd(s Scope) []string {
	return RefuseCmd(fmt.Sprintf("sandbox: unknown scope %q (want read-only, workspace-write, workspace-write-net or danger-full-access); refusing to run unsandboxed", string(s)))
}

// lookPath is indirected for tests. Tests can swap in a stub that returns
// ErrHelperMissing to exercise the graceful-degrade path.
var lookPath = exec.LookPath

// Wrap returns the OS-appropriate wrapped command for the given policy.
//
// If wrapping is impossible (helper missing, platform unsupported, or scope is
// DangerFullAccess) the original cmd is returned along with a non-nil error
// describing the reason. Callers should treat the error as a warning, not a
// fatal — the sandbox is hardening, not a security boundary.
func Wrap(p Policy, cmd []string) ([]string, error) {
	if len(cmd) == 0 {
		return cmd, errors.New("sandbox: empty command")
	}
	if p.Scope == DangerFullAccess || p.Scope == "" { // "" is unset, unconfined like Confines()
		return cmd, nil
	}
	if !p.Scope.known() {
		return unknownScopeCmd(p.Scope), fmt.Errorf("%w: %q", ErrUnknownScope, p.Scope)
	}
	switch runtime.GOOS {
	case "darwin":
		return wrapMacOS(p, cmd)
	case "linux":
		return wrapLinux(p, cmd)
	case "windows":
		// TODO(post-v0.1): detect WSL2, re-dispatch the command via
		// `wsl.exe -e ...` using the Linux bwrap path inside the distro. Until
		// then degrade to the bare command with a warning so the agent loop
		// can decide whether to proceed or abort based on its ApprovalMode.
		return cmd, fmt.Errorf("%w: windows sandbox not implemented (WSL2 dispatch post-v0.1)", ErrUnsupported)
	default:
		return cmd, ErrUnsupported
	}
}
