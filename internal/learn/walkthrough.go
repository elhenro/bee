package learn

import (
	"fmt"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

const summaryChars = 600

// Walkthrough replays a session as a review guide: per user request, the
// files the agent changed with its recorded reasons, then its closing words.
// It reads only what the transcript already holds.
func Walkthrough(msgs []types.Message, cwd string) string {
	var b strings.Builder
	turn := 0
	for i, m := range msgs {
		if m.Role != types.RoleUser || m.Ephemeral {
			continue
		}
		ask := Clean(firstLine(text(m)))
		if ask == "" {
			continue
		}
		end := len(msgs)
		for j := i + 1; j < len(msgs); j++ {
			if msgs[j].Role == types.RoleUser && text(msgs[j]) != "" {
				end = j
				break
			}
		}
		turn++
		fmt.Fprintf(&b, "%d. %s\n", turn, ask)
		for _, c := range Touched(msgs[i:end], cwd) {
			b.WriteString("   " + c.Path + "\n")
			for _, w := range c.Whys {
				b.WriteString("     why: " + w + "\n")
			}
		}
		if last := lastAssistantText(msgs[i:end]); last != "" {
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
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == types.RoleAssistant {
			if t := text(msgs[i]); t != "" {
				return strings.ReplaceAll(t, "\n", " ")
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
