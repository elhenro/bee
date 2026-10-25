// CLI session walkthrough. `bee explain [session-id]` replays a session as a
// review guide: what each request changed and the reasons the agent recorded.
package main

import (
	"fmt"
	"os"

	"github.com/elhenro/bee/internal/learn"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/types"
)

// resolveSession maps an optional id argument to a session id: empty,
// "latest" and "l" pick the newest.
func resolveSession(cmd string, args []string) string {
	if len(args) > 0 && args[0] != "" && args[0] != "latest" && args[0] != "l" {
		return args[0]
	}
	sessions, err := session.List()
	if err != nil {
		exitf(1, "bee %s: list sessions: %v", cmd, err)
	}
	if len(sessions) == 0 {
		exitf(1, "bee %s: no sessions found", cmd)
	}
	return sessions[0].ID
}

func loadSession(cmd string, args []string) (id string, msgs []types.Message, cwd string) {
	id = resolveSession(cmd, args)
	msgs, err := session.Read(id)
	if err != nil {
		exitf(1, "bee %s: %v", cmd, err)
	}
	cwd, _ = os.Getwd()
	return id, msgs, cwd
}

func runExplain(args []string) {
	id, msgs, cwd := loadSession("explain", args)
	out := learn.Walkthrough(msgs, cwd)
	if out == "" {
		exitf(1, "bee explain: session %s has nothing to explain", id)
	}
	fmt.Printf("session %s\n\n%s\n", learn.Clean(id), out)
}

// runQuiz is the opt-in active mode: guess each recorded reason, then see it.
func runQuiz(args []string) {
	id, msgs, cwd := loadSession("quiz", args)
	cards := learn.Cards(msgs, cwd, 5)
	if len(cards) == 0 {
		exitf(1, "bee quiz: session %s has no recorded reasons", id)
	}
	learn.Quiz(os.Stdin, os.Stdout, cards)
}
