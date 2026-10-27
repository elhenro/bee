package learn

import (
	"fmt"
	"slices"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

const summaryChars = 600

// Walkthrough replays a session as a review guide: per user request, the
// files the agent changed with its recorded reasons, then its closing words.
// It reads only what the transcript already holds.
func Walkthrough(msgs []types.Message, cwd string) string {
	var b strings.Builder
	n := 0
	for _, turn := range turns(msgs) {
		if !isRequest(turn[0]) {
			continue
		}
		ask := Clean(firstLine(text(turn[0])))
		if ask == "" {
			continue
		}
		n++
		fmt.Fprintf(&b, "%d. %s\n", n, ask)
		for _, c := range Touched(turn, cwd) {
			b.WriteString("   " + Clean(c.Path) + "\n")
			for _, w := range c.Whys {
				b.WriteString("     why: " + Clean(w) + "\n")
			}
		}
		if last := lastAssistantText(turn); last != "" {
			b.WriteString("   " + clip(Clean(last), summaryChars) + "\n")
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func text(m types.Message) string {
	var parts []string
	for _, c := range m.Content {
		if c.Type == types.BlockText && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func lastAssistantText(msgs []types.Message) string {
	for _, msg := range slices.Backward(msgs) {
		if msg.Role == types.RoleAssistant {
			if t := text(msg); t != "" {
				return t
			}
		}
	}
	return ""
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return clip(strings.TrimSpace(line), 100)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
