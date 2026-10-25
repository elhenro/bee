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

// capSummaryMaxTokens keeps the close-out short. the flattened tool-less
// request cannot reuse the prompt cache, so every output token here is the
// only cheap part of an otherwise full-price call.
const capSummaryMaxTokens = 400

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

	prompt := textMessage(types.RoleUser, lastID(res.Messages), "[iter cap reached] You've hit the iteration limit, so no more tools will run. "+
		"Stop calling tools. Give a final summary in under 120 words: what you changed, what is verified working, and what is still left to do.")

	// the tail (hive contract + pheromone card) stays in place so a capped
	// worker's close-out still arrives in report shape.
	sys, sysDynamic = appendPromptTail(sys, sysDynamic, e.promptTail())
	// same wire view the loop uses (card when active), then flattened to plain
	// text: the request carries no tools, and a history holding tool blocks
	// without tool specs is rejected by some wire formats.
	wire := dropEphemeral(append(append([]types.Message(nil), res.Messages...), prompt))
	if e.run != nil && e.run.card != nil {
		wire = e.run.card.view(wire)
	}
	req := llm.Request{
		Model:         e.Cfg.DefaultModel,
		System:        sys,
		SystemDynamic: sysDynamic,
		Messages:      flattenToolBlocks(wire),
		Stream:        true,
		Temperature:   0,
		MaxTokens:     capSummaryMaxTokens,
	}
	msg, text, _, err := e.streamOnce(ctx, req)
	if err != nil || strings.TrimSpace(text) == "" {
		return
	}
	// persist the prompt only now: a failed summary must not leave a
	// "no more tools will run" instruction in the rollout of a resumable run.
	if err := e.appendMessage(ctx, prompt); err != nil {
		return
	}
	res.Messages = append(res.Messages, prompt)
	msg.ParentID = prompt.ID
	if err := e.appendMessage(ctx, msg); err != nil {
		return
	}
	res.Messages = append(res.Messages, msg)
	res.FinalText = text
}

// flattenToolBlocks rewrites msgs as plain text turns: tool calls and results
// become bracketed text, thinking blocks are dropped, tool-role messages turn
// into user turns, and adjacent same-role turns merge. Used for requests that
// ship no tool specs.
func flattenToolBlocks(msgs []types.Message) []types.Message {
	out := make([]types.Message, 0, len(msgs))
	for _, m := range msgs {
		txt := flattenForSummary(m)
		if txt == "" {
			continue
		}
		role := m.Role
		if role == types.RoleTool {
			role = types.RoleUser
		}
		if n := len(out); n > 0 && out[n-1].Role == role {
			out[n-1].Content = []types.ContentBlock{textBlock(out[n-1].Content[0].Text + "\n" + txt)}
			continue
		}
		out = append(out, plainMessage(role, txt))
	}
	return out
}
