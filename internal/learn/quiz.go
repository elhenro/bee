package learn

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// Card is one retrieval prompt: a file the agent changed and the reason it
// recorded for the change.
type Card struct{ Path, Why string }

// Cards picks up to max changes that carry a recorded reason, across the
// whole session, newest request first.
func Cards(msgs []types.Message, cwd string, max int) []Card {
	var turns [][]types.Message
	start := 0
	for i, m := range msgs {
		if m.Role == types.RoleUser && !m.Ephemeral && text(m) != "" && i > start {
			turns = append(turns, msgs[start:i])
			start = i
		}
	}
	turns = append(turns, msgs[start:])
	var out []Card
	seen := map[string]bool{}
	for i := len(turns) - 1; i >= 0 && len(out) < max; i-- {
		for _, c := range Touched(turns[i], cwd) {
			if len(c.Whys) == 0 || seen[c.Path] || len(out) == max {
				continue
			}
			seen[c.Path] = true
			out = append(out, Card{Path: c.Path, Why: strings.Join(c.Whys, "; ")})
		}
	}
	return out
}

// Quiz asks the reader to predict each reason before showing it. Nothing is
// graded: writing the guess is the point, the reveal is the feedback.
func Quiz(in io.Reader, out io.Writer, cards []Card) {
	sc := bufio.NewScanner(in)
	for i, c := range cards {
		fmt.Fprintf(out, "%d/%d  %s changed. Why? (enter to skip)\n> ", i+1, len(cards), c.Path)
		if !sc.Scan() {
			fmt.Fprintln(out)
			return
		}
		fmt.Fprintf(out, "   agent: %s\n\n", c.Why)
	}
}
