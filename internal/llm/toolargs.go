package llm

import (
	"encoding/json"
	"strings"

	"github.com/elhenro/bee/internal/llm/wire"
	"github.com/elhenro/bee/internal/types"
	"github.com/google/uuid"
)

// decodeToolArgs parses a streamed tool-call argument string. Blank means no
// args. Malformed JSON comes back as a _parse_error/_raw_args input so the
// tool layer tells the model to re-emit instead of running with empty args.
func decodeToolArgs(raw string) map[string]any {
	input := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return input
	}
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return map[string]any{"_parse_error": err.Error(), "_raw_args": raw}
	}
	return input
}

// toolAdverts converts the request's tool specs to the wire translators' shape.
func toolAdverts(tools []ToolSpec) []wire.ToolAdvert {
	out := make([]wire.ToolAdvert, 0, len(tools))
	for _, t := range tools {
		out = append(out, wire.ToolAdvert{Name: t.Name, Description: t.Description, Schema: t.Schema})
	}
	return out
}

// newToolUse builds a tool_use event; an empty id gets a fresh one.
func newToolUse(id, name string, input map[string]any) Event {
	if id == "" {
		id = "call_" + uuid.NewString()
	}
	return Event{Type: EventToolUse, ToolUse: &types.ToolUse{ID: id, Name: name, Input: input}}
}
