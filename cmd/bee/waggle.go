// `bee waggle` inspects and curates the procedure-memory library: routes the
// agent crystallized from repeated read-only tool use during sessions.
package main

import (
	"fmt"
	"os"

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
		n1, _ := waggle.GC(proj)
		n2, _ := waggle.GC(user)
		fmt.Printf("waggle gc: removed %d project, %d user\n", n1, n2)
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
	fmt.Printf("[%s] %d waggle(s):\n", scope, len(metas))
	for _, m := range metas {
		fmt.Printf("  %s: %s\n      $ %s\n", m.Name, m.Description, m.Script)
	}
}
