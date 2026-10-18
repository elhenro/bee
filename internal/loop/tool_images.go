package loop

import "github.com/elhenro/bee/internal/types"

// toolImageBlocks collects the pictures this batch's tools produced, in call
// order. They ride in the tool message after every tool_result and warning
// (wires put tool results first), so the model sees them next iteration; the
// result's text says what they are.
func toolImageBlocks(results []types.ToolResult) []types.ContentBlock {
	var out []types.ContentBlock
	for _, r := range results {
		if r.IsError {
			continue
		}
		out = append(out, r.Images...)
	}
	return out
}
