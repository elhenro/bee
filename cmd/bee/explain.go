// CLI session walkthrough. `bee explain [session-id]` replays a session as a
// review guide: what each request changed and the reasons the agent recorded.
package main

import (
	"fmt"
	"os"

	"github.com/elhenro/bee/internal/learn"
	"github.com/elhenro/bee/internal/session"
)

func runExplain(args []string) { explainSession(args, false) }

// runQuiz is the opt-in active mode: guess each recorded reason, then see it.
func runQuiz(args []string) { explainSession(args, true) }

func explainSession(args []string, quiz bool) {
	id := ""
	if len(args) > 0 {
		id = args[0]
	}
	if id == "" {
		sessions, err := session.List()
		if err != nil || len(sessions) == 0 {
			exitf(1, "bee explain: no sessions found")
		}
		id = sessions[0].ID
	}
	msgs, err := session.Read(id)
	if err != nil {
		exitf(1, "bee explain: %v", err)
	}
	cwd, _ := os.Getwd()
	if quiz {
		cards := learn.Cards(msgs, cwd, 5)
		if len(cards) == 0 {
			exitf(1, "bee quiz: session %s has no recorded reasons", id)
		}
		learn.Quiz(os.Stdin, os.Stdout, cards)
		return
	}
	out := learn.Walkthrough(msgs, cwd)
	if out == "" {
		exitf(1, "bee explain: session %s has nothing to explain", id)
	}
	fmt.Printf("session %s\n\n%s\n", learn.Clean(id), out)
}
