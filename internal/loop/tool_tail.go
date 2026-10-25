package loop

import (
	"context"

	"github.com/elhenro/bee/internal/tools"
)

// ToolTail is the last few output rows of a tool that is still running.
type ToolTail struct {
	UseID string
	Text  string
}

// withToolTail lets a running tool report its output tail on ToolTailCh. ctx
// comes back unchanged when no UI listens.
func (e *Engine) withToolTail(ctx context.Context, useID string) context.Context {
	if e.ToolTailCh == nil {
		return ctx
	}
	return tools.WithLiveSink(ctx, func(text string) {
		trySend(e.ToolTailCh, ToolTail{UseID: useID, Text: text})
	})
}
