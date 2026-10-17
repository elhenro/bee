package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	hivepkg "github.com/elhenro/bee/internal/hive"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/recipe"
	"github.com/elhenro/bee/internal/skills"
)

// engineHost adapts a headless engine to recipe.Host. Tool and shell steps run
// through Engine.CallTool so sandbox, approval and truncation apply; bee steps
// run on a spawned worker so the parent's transcript stays clean and each bee
// turn starts from only its prompt.
type engineHost struct{ eng *loop.Engine }

func (h engineHost) RunTool(ctx context.Context, name string, input map[string]any) (string, bool, error) {
	res, err := h.eng.CallTool(ctx, name, input)
	if err != nil {
		return "", true, err
	}
	return res.Content, res.IsError, nil
}

func (h engineHost) RunBee(ctx context.Context, prompt string, allow []string) (string, error) {
	w, roll, err := hivepkg.SpawnWorker(h.eng, "recipe")
	if err != nil {
		return "", err
	}
	defer roll.Close()
	if len(allow) > 0 {
		if w.Tools, err = filterTools(h.eng.Tools, strings.Join(allow, ",")); err != nil {
			return "", err
		}
	}
	// the model's streamed text goes to stderr with the progress log; stdout
	// is reserved for the recipe's final output so `bee <recipe> > out` is clean.
	w.Stdout = os.Stderr
	w.Costs = h.eng.Costs
	turnCtx, cancel := context.WithTimeout(ctx, h.eng.Cfg.Watchdog.Timeout())
	defer cancel()
	res, err := w.Run(turnCtx, prompt)
	if err != nil {
		return "", err
	}
	fmt.Fprintln(os.Stderr)
	return strings.TrimSpace(res.FinalText), nil
}

// runRecipeHeadless executes a runtime recipe on eng and prints the last
// step's output to stdout. Returns the process exit code: 0 ok, 8 when a step
// escalated to the user (matching the engine's escalate code), 1 on failure.
func runRecipeHeadless(eng *loop.Engine, s skills.Skill, args []string) int {
	// recipe steps are already crystallized procedure; recording them would
	// only teach the miner routes it has no business re-deriving.
	eng.Waggle = nil
	eng.Replay = nil
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	res, err := recipe.Run(ctx, engineHost{eng}, s, recipe.Options{Args: args, Log: os.Stderr})
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee %s: %v\n", s.Name, err)
		return 1
	}
	if res.Output != "" {
		fmt.Fprintln(os.Stdout, strings.TrimRight(res.Output, "\n"))
	}
	switch res.Status {
	case recipe.StatusEscalate:
		fmt.Fprintf(os.Stderr, "bee %s: escalated: %s\n", s.Name, res.Reason)
		return 8
	case recipe.StatusFailed:
		fmt.Fprintf(os.Stderr, "bee %s: failed: %s\n", s.Name, res.Reason)
		return 1
	}
	return 0
}
