package goal

import (
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
)

// EvalTimeout caps one judge call so a stuck provider can't wedge the loop. A
// local model judging cold prefills the whole transcript before its first
// token, which easily passes half a minute, so the cap is generous.
const EvalTimeout = 2 * time.Minute

// judgeThinking switches reasoning off for the judge. The verdict is one line;
// a hybrid reasoning model that thinks first spends the token budget (and tens
// of seconds at local speeds) before it can emit the VERDICT line. system gets
// the literal toggle token, kwargs the template switch local servers honor
// (hosted endpoints may reject chat_template_kwargs, so it stays local-only).
func judgeThinking(p llm.Provider, model, system string) (string, map[string]any) {
	if !llm.IsQwen3HybridThinking(model) {
		return system, nil
	}
	system += "\n\n" + llm.Qwen3ThinkingHint(llm.ThinkingOff)
	if config.IsLocalProvider(p.Name()) {
		return system, map[string]any{"enable_thinking": false}
	}
	return system, nil
}
