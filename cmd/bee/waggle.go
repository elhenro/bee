// `bee waggle` inspects and curates the procedure-memory library: routes the
// agent crystallized from repeated read-only tool use during sessions.
package main

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/elhenro/bee/internal/waggle"
)

func runWaggle(args []string) {
	sub := "ls"
	if len(args) > 0 {
		sub = args[0]
	}
	cwd, _ := os.Getwd()
	proj, errP := waggle.ProjectStore(cwd)
	user, errU := waggle.UserStore()
	if errP != nil || errU != nil {
		fmt.Fprintf(os.Stderr, "bee waggle: %v %v\n", errP, errU)
		os.Exit(1)
	}
	switch sub {
	case "ls":
		waggleLs("project", proj)
		waggleLs("user", user)
	case "gc":
		// full curation pass: gc dups, demote chronic divergers, prune stale,
		// compact orphaned ledger history, promote cross-project routes.
		const staleAge = 14 * 24 * time.Hour
		const minDivergence = 3
		r, _ := waggle.Curate(proj, user, staleAge, minDivergence, time.Now())
		fmt.Printf("waggle gc: removed %d project, %d user; pruned %d stale; demoted %d diverging; promoted %d to user\n",
			r.RemovedProj+r.PrunedProj, r.RemovedUser+r.PrunedUser, r.PrunedProj+r.PrunedUser, r.Demoted, r.Promoted)
	default:
		fmt.Fprintf(os.Stderr, "bee waggle: unknown subcommand %q (want ls|gc)\n", sub)
		os.Exit(2)
	}
}

func waggleLs(scope string, s *waggle.Store) {
	metas, err := waggle.List(s)
	if err != nil {
		fmt.Fprintf(os.Stderr, "waggle ls (%s): %v\n", scope, err)
		return
	}
	if len(metas) == 0 {
		fmt.Printf("[%s] no waggles\n", scope)
		return
	}
	// rank by yield (estimated tokens saved): the leaderboard surfaces the
	// routes actually paying off, not just what got crystallized.
	stats, _ := waggle.ReadLedger(s.LedgerPath())
	sort.SliceStable(metas, func(i, j int) bool {
		return stats[metas[i].Name].Yield > stats[metas[j].Name].Yield
	})
	fmt.Printf("[%s] %d waggle(s):\n", scope, len(metas))
	for _, m := range metas {
		st := stats[m.Name]
		fmt.Printf("  %s  (uses %d, ~%d tok saved): %s\n      $ %s\n", m.Name, st.Uses, st.Yield, m.Description, m.Script)
	}
}
