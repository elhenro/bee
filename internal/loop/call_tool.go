package loop

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/elhenro/bee/internal/types"
)

var callToolSeq atomic.Int64

// CallTool runs one registered tool outside a model turn, through the same
// path a model-issued call takes: schema validation, sandbox wrapping for
// shell, secret redaction and output truncation. Recipe runtimes use it so a
// deterministic step gets every guard a model call would.
//
// The per-turn tool gate is not applied: it reflects what the model was
// advertised on the last turn, and this call is not the model's.
func (e *Engine) CallTool(ctx context.Context, name string, input map[string]any) (types.ToolResult, error) {
	id := fmt.Sprintf("call-%d", callToolSeq.Add(1))
	if e.Tools == nil {
		return types.ToolResult{UseID: id, Content: "no tools registered", IsError: true}, nil
	}
	if _, ok := e.Tools.Get(name); !ok {
		return types.ToolResult{UseID: id, Content: unknownToolMsg(name, e.Tools.Names()), IsError: true}, nil
	}
	prev := e.run
	e.run = nil
	defer func() { e.run = prev }()
	return e.runOne(ctx, types.ToolUse{ID: id, Name: name, Input: input})
}
