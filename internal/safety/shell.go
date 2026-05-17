package safety

import (
	"fmt"
	"regexp"
	"strings"
)

// rm -rf / and friends. matches the recursive+force flag pair (in either
// order, combined or separate) followed by a quoted-or-bare /.
var rmRfRoot = regexp.MustCompile(
	`\brm\s+(-[a-zA-Z]*r[a-zA-Z]*f[a-zA-Z]*|-[a-zA-Z]*f[a-zA-Z]*r[a-zA-Z]*|--recursive\s+--force|--force\s+--recursive)\s+(['"]?/['"]?\s*($|;|&|\|))`,
)

var ddToDisk = regexp.MustCompile(`(?i)\bdd\b[^|]*\bof=/dev/(disk|sd|nvme|hd)`)
var diskFormat = regexp.MustCompile(`\b(mkfs(\.[a-z0-9]+)?|fdisk|parted)\b`)
var diskutilErase = regexp.MustCompile(`(?i)\bdiskutil\s+erase`)

// CheckShellCommand heuristically blocks commands that almost certainly mean
// the model went off the rails. user approval is the primary gate; this just
// catches a few catastrophic shapes. matches happen on the trimmed command
// string, so leading/trailing whitespace is ignored.
func CheckShellCommand(cmd string) error {
	c := strings.TrimSpace(cmd)
	if c == "" {
		return nil
	}
	if rmRfRoot.MatchString(c) {
		return fmt.Errorf("refused: command attempts to recursively delete filesystem root")
	}
	if strings.Contains(c, "--no-preserve-root") {
		return fmt.Errorf("refused: --no-preserve-root is not allowed")
	}
	if ddToDisk.MatchString(c) {
		return fmt.Errorf("refused: dd to a block device is not allowed")
	}
	if diskFormat.MatchString(c) || diskutilErase.MatchString(c) {
		return fmt.Errorf("refused: disk-formatting commands are not allowed")
	}
	return nil
}
