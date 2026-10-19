package loop

import (
	"context"
	"fmt"
	"maps"

	"github.com/elhenro/bee/internal/ask"
)

// askVisionSupport asks the user once whether the active model accepts images,
// for models neither the config, the provider catalogue nor the name list can
// classify (typically local servers that don't declare input types). The
// answer is kept in Cfg.Vision.Models and saved through PersistVision, so it
// is asked once per model. A dismissal or a missing asker counts as "no" for
// now and is not asked again this session.
func (e *Engine) askVisionSupport(ctx context.Context) bool {
	id := e.Cfg.DefaultModel
	if e.VisionAsker == nil || e.visionAsked[id] {
		return false
	}
	if e.visionAsked == nil {
		e.visionAsked = map[string]bool{}
	}
	e.visionAsked[id] = true
	ans, err := e.VisionAsker.Ask(ctx, ask.Question{
		Header: "image support",
		Prompt: fmt.Sprintf("bee can't tell whether %q accepts images. does it?", id),
		Options: []ask.Option{
			{Label: "yes, it sees images", Description: "send images straight to the model; remembered for this model"},
			{Label: "no, text only", Description: "describe images via the [vision] fallback or drop them; remembered"},
		},
	})
	if err != nil || ans.Dismissed || ans.Index < 0 {
		return false
	}
	has := ans.Index == 0
	// copy-on-write: Cfg copies handed to other engines share the old map
	models := maps.Clone(e.Cfg.Vision.Models)
	if models == nil {
		models = map[string]bool{}
	}
	models[id] = has
	e.Cfg.Vision.Models = models
	if e.PersistVision != nil {
		if err := e.PersistVision(id, has); err != nil {
			e.warnf("could not save the image-support answer: %v", err)
		}
	}
	return has
}
