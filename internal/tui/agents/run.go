package agents

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/agents"
)

// RunOverview launches the overview program in a fresh alt-screen. Returns
// Result.AttachID when the user picked an agent (caller opens `bee back`).
// Spawns a merger goroutine that auto-merges done branches on a ticker.
func RunOverview(repoRoot string) (Result, error) {
	mergerCtx, mergerCancel := context.WithCancel(context.Background())
	defer mergerCancel()
	go agents.MergerLoop(mergerCtx, repoRoot, 10*time.Second)

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
