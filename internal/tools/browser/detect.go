// Package browser provides native chromedp-backed browser tools so bee
// agents can open, snapshot, click, and read the console of the page they
// are building. Drives an existing Chrome/Chromium install; never bundles a
// browser. Opt-in via [browser] config or --browser.
package browser

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrNotFound means no Chrome/Chromium binary could be located.
var ErrNotFound = errors.New("browser: no Chrome/Chromium binary found")

// DetectChrome returns a usable browser binary path. override wins when
// non-empty and existing; otherwise known install locations and $PATH are
// probed. Returns ErrNotFound if nothing is usable.
func DetectChrome(override string) (string, error) {
	if override != "" {
		if isExec(override) {
			return override, nil
		}
		return "", errors.New("browser: chrome_path does not exist or is not executable: " + override)
	}
	for _, p := range candidatePaths() {
		if isExec(p) {
			return p, nil
		}
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", ErrNotFound
}

func candidatePaths() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	default:
		return []string{
			"/usr/bin/google-chrome",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
		}
	}
}

func isExec(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	// Windows: os.Stat may not return execute bits for all files,
	// so we also accept files that exist and have a common executable extension.
	if fi.Mode()&0o111 != 0 {
		return true
	}
	ext := strings.ToLower(filepath.Ext(p))
	return ext == ".exe" || ext == ".bat" || ext == ".cmd" || ext == ""
}
