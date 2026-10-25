// `bee agents` subcommand — opens the parallel-agents overview. Each chat
// submission spawns a detached headless agent in its own git worktree; the
// overview lists status + tokens + last thought across all of them.
package main

import (
	"fmt"
	"os"

	tuiagents "github.com/elhenro/bee/internal/tui/agents"
	"github.com/elhenro/bee/internal/zzz"
)

// runAgents is the entry point wired into main.go; it takes no flags yet.
func runAgents() {
	cwd, err := os.Getwd()
	if err != nil {
		exitf(1, "bee agents: cwd: %v", err)
	}
	repoRoot, err := zzz.RepoRoot(cwd)
	if err != nil {
		exitf(1, "bee agents: not inside a git repository — agents need a git worktree to operate on.")
	}

	// loop: overview → attach (bee back) → overview → ... until ctrl+c at overview
	for {
		res, err := tuiagents.RunOverview(repoRoot)
		if err != nil {
			exitf(1, "bee agents: %v", err)
		}
		if res.AttachID == "" {
			return // clean quit
		}
		// hand off to the existing `bee back` flow in-process. When that
		// returns (user quit the session view) we loop back to the overview.
		// BEE_FROM_AGENTS lets the TUI know left-arrow should quit back here
		// instead of opening the in-TUI agent overlay.
		fmt.Fprintf(os.Stderr, "\nattaching to %s — press left arrow or ctrl+c twice to return to overview.\n", res.AttachID)
		_ = os.Setenv("BEE_FROM_AGENTS", "1")
		runTUIWithSession(res.AttachID, "")
		_ = os.Unsetenv("BEE_FROM_AGENTS")
	}
}
