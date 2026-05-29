package bench

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// FinalMessageFile is the sandbox file the runner writes the model's last
// assistant text into. Abstain/refusal tasks grep this to confirm the model
// said it cannot do the request instead of fabricating a result.
const FinalMessageFile = ".bee_final_message"

// writeFinalMessage concatenates the text blocks of the last assistant message
// and writes them into the sandbox. Best effort: a missing file simply fails
// any grep check pointed at it, which is the correct verdict.
func writeFinalMessage(sandbox string, msgs []types.Message) {
	var text string
	for _, msg := range msgs {
		if msg.Role != types.RoleAssistant {
			continue
		}
		var parts []string
		for _, b := range msg.Content {
			if b.Type == types.BlockText && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		if len(parts) > 0 {
			text = strings.Join(parts, "\n") // keep last assistant turn only
		}
	}
	_ = os.WriteFile(filepath.Join(sandbox, FinalMessageFile), []byte(text), 0o644)
}

// AttachHoldout runs the held-out task slice with the same options and folds its
// aggregate into res under the Holdout* fields. The held-out tasks reuse the
// exact scoring/checks machinery — this only relabels and segregates the
// scoreboard so a tuner can compare main-suite delta vs held-out delta in one
// run. When dir is missing or empty, res is left untouched (backward compatible).
func AttachHoldout(ctx context.Context, res *SuiteResult, dir string, opt Options) error {
	tasks, err := LoadTasks(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // no held-out slice configured
		}
		return err
	}
	if len(tasks) == 0 {
		return nil
	}
	hopt := opt
	hopt.Label = opt.Label + "-holdout"
	hres, err := RunSuite(ctx, tasks, hopt)
	if err != nil {
		return err
	}
	res.HoldoutAggregate = hres.Aggregate
	res.HoldoutDimMeans = hres.DimMeans
	res.HoldoutTasks = hres.Tasks
	return nil
}
