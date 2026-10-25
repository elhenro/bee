package loop

import (
	"maps"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
)

// unscaledProfile holds the two budgets config.ScaleProfileForContext widens.
type unscaledProfile struct {
	name       string
	promptBudg int
	toolOutTok int
}

// rescaleProfile applies the context-window scaling for the current model,
// first undoing a previous scale so switching to a smaller window (or a model
// that does not scale) does not keep the larger budgets.
func (e *Engine) rescaleProfile() {
	if u := e.profileUnscaled; u != nil {
		if p, ok := e.Cfg.Profiles[u.name]; ok {
			e.Cfg.Profiles = maps.Clone(e.Cfg.Profiles)
			p.SystemPromptBudget = u.promptBudg
			p.ToolOutputTokens = u.toolOutTok
			e.Cfg.Profiles[u.name] = p
		}
		e.profileUnscaled = nil
	}
	name := config.ResolveAutoProfileForProvider(e.Cfg.DefaultProvider, e.Cfg.DefaultModel)
	if name != "tiny" {
		return
	}
	ctxWindow := llm.ContextWindow(e.Cfg.DefaultModel)
	if ctxWindow <= 16000 {
		return
	}
	resolved := e.Cfg.Profile
	if resolved == "auto" {
		resolved = name
	}
	p, ok := e.Cfg.Profiles[resolved]
	if !ok {
		return
	}
	e.Cfg.Profiles = maps.Clone(e.Cfg.Profiles)
	e.profileUnscaled = &unscaledProfile{name: resolved, promptBudg: p.SystemPromptBudget, toolOutTok: p.ToolOutputTokens}
	e.Cfg.Profiles[resolved] = config.ScaleProfileForContext(p, resolved, ctxWindow)
}
