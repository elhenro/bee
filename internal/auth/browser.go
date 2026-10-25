package auth

import (
	"os/exec"
	"runtime"
)

// openURL opens u in the user's default browser via OS-specific helper.
// Best-effort: caller falls back to printing the URL on error. A var so tests
// can stub the browser.
var openURL = func(u string) error {
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
