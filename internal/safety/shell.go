package safety

import (
	"fmt"
	"regexp"
	"strings"
)

// rm -rf / and friends. matches the recursive+force flag pair (in either
// order, combined or separate) followed by a quoted-or-bare /.
var rmRfRoot = regexp.MustCompile(
	`\brm\s+(-[a-zA-Z]*r[a-zA-Z]*f[a-zA-Z]*|-[a-zA-Z]*f[a-zA-Z]*r[a-zA-Z]*|-[rf]\s+-[rf]|--recursive\s+--force|--force\s+--recursive)\s+(['"]?/\*?['"]?\s*($|;|&|\|))`,
)

var ddToDisk = regexp.MustCompile(`(?i)\bdd\b[^|]*\bof=/dev/(disk|sd|nvme|hd)`)
var diskFormat = regexp.MustCompile(`\b(mkfs(\.[a-z0-9]+)?|fdisk|parted)\b`)
var diskutilErase = regexp.MustCompile(`(?i)\bdiskutil\s+erase`)

// CheckShellCommand heuristically blocks commands that almost certainly mean
// the model went off the rails. user approval is the primary gate; this just
// catches a few catastrophic shapes. Each form (raw + shell-normalized) is
// checked so quoting/backslash/$IFS splices can't slip a catastrophic command
// past the regexes.
func CheckShellCommand(cmd string) error {
	for _, c := range matchForms(cmd) {
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
	}
	return nil
}

// matchForms returns the strings a denylist/hardline regex should be tested
// against: the trimmed raw command plus a shell-normalized view. Both are
// returned so normalization only ever ADDS matches and can never mask a command
// that already matches in raw form. Empty results are dropped.
func matchForms(cmd string) []string {
	raw := strings.TrimSpace(cmd)
	if raw == "" {
		return nil
	}
	norm := strings.TrimSpace(normalizeForMatch(raw))
	if norm == "" || norm == raw {
		return []string{raw}
	}
	return []string{raw, norm}
}

// normalizeForMatch approximates bash's own normalization, for matching only:
// it undoes quote splices (rm'' -rf), backslash splices (r\m), and $IFS
// word-splitting (rm$IFS-rf). Heuristic and intentionally lossy; callers must
// also check the raw form (see matchForms).
func normalizeForMatch(cmd string) string {
	return strings.NewReplacer(
		"${IFS}", " ",
		"$IFS", " ",
		`\`, "",
		`'`, "",
		`"`, "",
	).Replace(cmd)
}
