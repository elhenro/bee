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

// Cards picks up to limit changes that carry a recorded reason, across the
// whole session, newest request first.
func Cards(msgs []types.Message, cwd string, limit int) []Card {
	ts := turns(msgs)
	var out []Card
	seen := map[string]bool{}
	for i := len(ts) - 1; i >= 0 && len(out) < limit; i-- {
		for _, c := range Touched(ts[i], cwd) {
			if len(c.Whys) == 0 || seen[c.Path] || len(out) == limit {
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
		fmt.Fprintf(out, "%d/%d  %s changed. Why? (enter to skip)\n> ", i+1, len(cards), Clean(c.Path))
		if !sc.Scan() {
			fmt.Fprintln(out)
			return
		}
		fmt.Fprintf(out, "   agent: %s\n\n", Clean(c.Why))
	}
}
