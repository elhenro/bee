package main

import (
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/muesli/termenv"

	"github.com/elhenro/bee/internal/types"
)

// detectDarkBg picks lipgloss bg mode.
//   - BEE_THEME=light|dark forces it
//   - else COLORFGBG=<fg>;<bg> — bg<7 = dark (xterm-ish convention)
//   - else OSC 11 query via termenv (runs BEFORE bubbletea grabs stdin
//     so the terminal reply is consumed cleanly)
//   - else default dark
func detectDarkBg() bool {
	if t := strings.ToLower(strings.TrimSpace(os.Getenv("BEE_THEME"))); t != "" {
		return t != "light"
	}
	if v := os.Getenv("COLORFGBG"); v != "" {
		parts := strings.Split(v, ";")
		if n, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			return n < 7
		}
	}
	// termenv.HasDarkBackground sends OSC 11 and reads the reply. Safe here
	// because we're called before tea.NewProgram puts stdin in raw mode.
	// Falls back to true on any error (no TTY, query timeout, etc.).
	return termenv.HasDarkBackground()
}

func stripAgentPreambleMessages(msgs []types.Message) []types.Message {
	if len(msgs) == 0 || os.Getenv("BEE_AGENT_WORKTREE") == "1" {
		return msgs
	}
	out := slices.Clone(msgs)
	for i := range out {
		if out[i].Role != types.RoleUser {
			continue
		}
		changed := false
		blocks := slices.Clone(out[i].Content)
		for j := range blocks {
			if blocks[j].Type != types.BlockText {
				continue
			}
			if cleaned, ok := stripAgentPreambleText(blocks[j].Text); ok {
				blocks[j].Text, changed = cleaned, true
			}
		}
		if changed {
			out[i].Content = blocks
		}
	}
	return out
}

func stripAgentPreambleText(s string) (string, bool) {
	for _, marker := range []string{"\n\nTask:\n", "\r\n\r\nTask:\r\n"} {
		prefix, rest, found := strings.Cut(s, marker)
		if found && strings.Contains(prefix, "You are running unattended as one of many parallel") &&
			strings.Contains(prefix, "DONE: <one-line summary>") {
			return strings.TrimLeft(rest, " \t\r\n"), true
		}
	}
	return s, false
}
