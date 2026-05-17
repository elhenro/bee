package auth

import (
	"os/exec"
	"runtime"
)

// openURLFn is the indirection used by Login so tests can stub browser open.
var openURLFn = openURLDefault

// openURL is the actual fn used by Login. Indirected through openURLFn for tests.
func openURL(u string) error { return openURLFn(u) }

// openURLDefault opens u in the user's default browser via OS-specific helper.
// Best-effort: caller falls back to printing the URL on error.
func openURLDefault(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	return cmd.Start()
}
