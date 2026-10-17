package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/gate"
	"github.com/elhenro/bee/internal/zzz"
)

// zzzGate resolves the verify commands for the repo under cwd and says which
// gate the run will use, so an ungated overnight run is never a surprise.
func zzzGate(cfg config.Config) []string {
	cwd, _ := os.Getwd()
	root, err := zzz.RepoRoot(cwd)
	if err != nil {
		root = cwd
	}
	cmds := gate.Commands(root, cfg.Swarm.Verify)
	if len(cmds) == 0 {
		fmt.Fprintf(os.Stderr, "[zzz] no verify gate (%s or [swarm] verify): commits land untested\n", gate.File)
		return nil
	}
	fmt.Fprintf(os.Stderr, "[zzz] verify gate: %s\n", strings.Join(cmds, " && "))
	return cmds
}
