package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// buildInvocation picks the shell binary and wraps cmdStr with an optional
// rc-source prelude when Options.UseUserRC is set. Default returns ("bash",
// cmdStr) — unchanged hermetic behavior.
func (t *Tool) buildInvocation(cmdStr string) (string, string) {
	if !t.opts.UseUserRC {
		return "bash", cmdStr
	}
	shellBin := t.opts.Shell
	if shellBin == "" {
		shellBin = os.Getenv("SHELL")
	}
	if shellBin == "" {
		shellBin = "bash"
	}
	rc := t.opts.RCFile
	if rc == "" {
		rc = defaultRCFor(shellBin)
	}
	if rc == "" {
		return shellBin, cmdStr
	}
	// non-interactive shells alias-expand at parse time, so an alias defined
	// while sourcing the rc is invisible to commands tokenized in the same
	// -c string. Workaround: source rc, then run the user command via `eval`
	// — eval parses its argument after the alias table is populated. bash
	// additionally needs `shopt -s expand_aliases` to honor aliases under
	// non-interactive mode at all. The [ -f rc ] guard keeps missing rc
	// files silent for first-run users.
	quotedRC := shellQuote(rc)
	prelude := fmt.Sprintf("[ -f %s ] && . %s 2>/dev/null; ", quotedRC, quotedRC)
	if filepath.Base(shellBin) == "bash" {
		prelude = "shopt -s expand_aliases 2>/dev/null; " + prelude
	}
	return shellBin, prelude + "eval " + shellQuote(cmdStr)
}

// defaultRCFor maps the shell binary to the canonical interactive rc file.
// Returns "" for unsupported shells (caller falls through to no prelude).
func defaultRCFor(shellBin string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	switch filepath.Base(shellBin) {
	case "zsh":
		return filepath.Join(home, ".zshrc")
	case "bash":
		return filepath.Join(home, ".bashrc")
	case "fish":
		return filepath.Join(home, ".config", "fish", "config.fish")
	}
	return ""
}

// shellQuote wraps s in single quotes for safe inclusion in a shell command,
// escaping any embedded single quotes via the standard '\” dance.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
