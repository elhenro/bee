package loop

import (
	"context"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

// capSummaryTimeout bounds the wind-down call so a model already at its iter
// ceiling can't hang the exit.
const capSummaryTimeout = 30 * time.Second

// finalSummaryOnCap runs one tool-less turn when the loop hits MaxIterations so
// the user gets a concise close-out (what changed, what works, what's left)
// instead of an abrupt error. Best-effort: any failure leaves res untouched and
// the caller still surfaces MaxIterationsError. The summary is appended to the
// transcript and res.FinalText so the wrapper shows it.
func (e *Engine) finalSummaryOnCap(ctx context.Context, res *RunResult, sys, sysDynamic string) {
	if e.Provider == nil || len(res.Messages) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, capSummaryTimeout)
	defer cancel()

	prompt := types.Message{
		ID:       newID(),
		ParentID: lastID(res.Messages),
		Role:     types.RoleUser,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: "[iter cap reached] You've hit the iteration limit, so no more tools will run. " +
			"Stop calling tools. Give a short final summary: what you changed, what is verified working, and what is still left to do."}},
		Time: time.Now().UTC(),
	}
	if err := e.appendMessage(ctx, prompt); err != nil {
		return
	}
	res.Messages = append(res.Messages, prompt)

	req := llm.Request{
		Model:         e.Cfg.DefaultModel,
		System:        sys,
		SystemDynamic: sysDynamic,
		Messages:      dropEphemeral(res.Messages),
		Stream:        true,
		Temperature:   0,
	}
	msg, text, _, err := e.streamOnce(ctx, req)
	if err != nil || strings.TrimSpace(text) == "" {
		return
	}
	msg.ParentID = lastID(res.Messages)
	if err := e.appendMessage(ctx, msg); err != nil {
		return
	}
	res.Messages = append(res.Messages, msg)
	res.FinalText = text
}
