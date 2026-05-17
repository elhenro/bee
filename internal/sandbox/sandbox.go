package sandbox

import (
	"errors"
	"runtime"
)

// ErrUnsupported signals that wrapping is not implemented for this platform.
// Wrap returns this alongside the unwrapped command — the caller decides
// whether to abort or proceed with degraded confinement.
var ErrUnsupported = errors.New("sandbox: platform not supported")

// ErrHelperMissing signals that the OS-level helper (bwrap, sandbox-exec) is
// not on PATH. The original cmd is returned so the caller can degrade.
var ErrHelperMissing = errors.New("sandbox: helper binary not found")

// lookPath is indirected for tests. Tests can swap in a stub that returns
// ErrHelperMissing to exercise the graceful-degrade path.
var lookPath = defaultLookPath

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
	if p.Scope == DangerFullAccess {
		return cmd, nil
	}
	switch runtime.GOOS {
	case "darwin":
		return wrapMacOS(p, cmd)
	case "linux":
		return wrapLinux(p, cmd)
	case "windows":
		return wrapWindows(p, cmd)
	default:
		return cmd, ErrUnsupported
	}
}
