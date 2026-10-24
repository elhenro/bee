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
			fmt.Fprintln(os.Stderr, "bee explain: no sessions found")
			os.Exit(1)
		}
		id = sessions[0].ID
	}
	msgs, err := session.Read(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee explain: %v\n", err)
		os.Exit(1)
	}
	cwd, _ := os.Getwd()
	if quiz {
		cards := learn.Cards(msgs, cwd, 5)
		if len(cards) == 0 {
			fmt.Fprintf(os.Stderr, "bee quiz: session %s has no recorded reasons\n", id)
			os.Exit(1)
		}
		learn.Quiz(os.Stdin, os.Stdout, cards)
		return
	}
	out := learn.Walkthrough(msgs, cwd)
	if out == "" {
		fmt.Fprintf(os.Stderr, "bee explain: session %s has nothing to explain\n", id)
		os.Exit(1)
	}
	fmt.Printf("session %s\n\n%s\n", id, out)
}
