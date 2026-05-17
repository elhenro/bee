package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// helperName is the basename we look for in argv[0] to decide whether the
// current process is running as the sandbox helper. Single-binary re-exec
// avoids shipping a second executable.
const helperName = "bee-sandbox-helper"

// defaultLookPath is the production exec.LookPath. Wrap functions go through
// the package-level lookPath var so tests can stub helper-missing scenarios.
func defaultLookPath(file string) (string, error) {
	return exec.LookPath(file)
}

// IsHelper reports whether argv[0] identifies this process as the sandbox
// helper. Callers should branch to HelperMain at the very top of main() when
// this returns true, before any normal CLI setup.
func IsHelper() bool {
	if len(os.Args) == 0 {
		return false
	}
	return strings.HasSuffix(filepath.Base(os.Args[0]), helperName)
}

// HelperMain is the entry point when bee is invoked as bee-sandbox-helper.
// Wave 2 will fill this in (parse policy from env, apply scope-specific
// confinement, exec the inner command). For now it's a placeholder that
// errors loudly so a misconfigured wave-2 invocation doesn't silently run
// unsandboxed.
func HelperMain() error {
	return fmt.Errorf("sandbox: helper invoked but not yet implemented (wave 2)")
}

// HelperPath returns the symlink/argv0 alias to use when re-exec'ing bee as
// the helper. Resolves /proc/self/exe (or os.Executable) and returns its
// directory joined with helperName. The caller is expected to either symlink
// to this path or pass it as argv[0] to the exec syscall.
func HelperPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("sandbox: locate self: %w", err)
	}
	dir := filepath.Dir(exe)
	return filepath.Join(dir, helperName), nil
}
