package agents

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/agents"
	"github.com/elhenro/bee/internal/config"
)

// RunOverview launches the overview program in a fresh alt-screen. Returns
// Result.AttachID when the user picked an agent (caller opens `bee back`).
// Spawns a merger goroutine that auto-merges done branches on a ticker.
func RunOverview(repoRoot string) (Result, error) {
	mergerCtx, mergerCancel := context.WithCancel(context.Background())
	defer mergerCancel()
	// a missing api key still leaves the merged [swarm] section in cfg, and
	// the merger never calls a model, so the error is not fatal here.
	cfg, _ := config.Load()
	go agents.MergerLoop(mergerCtx, repoRoot, 10*time.Second, cfg.Swarm)

	m := newModel(repoRoot)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return Result{}, err
	}
	if mm, ok := final.(model); ok {
		return Result{AttachID: mm.attachID}, nil
	}
	return Result{}, nil
}
