package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
)

// prewarmLocalContext learns the active local model's context window before
// the first turn so contextBudget and the tiny-profile scaling start from
// reality. Synchronous on purpose: a closed port fails fast on dial and a
// running server answers well under a second.
func prewarmLocalContext(cfg config.Config) {
	pc, ok := cfg.Providers[cfg.DefaultProvider]
	if !ok || pc.BaseURL == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), localContextProbeTimeout)
	defer cancel()

	// /api/show first: its num_ctx is what the runtime really allocates.
	// servers without it (omlx, lm-studio, vllm) 404 here, which is not a failure.
	if n, err := llm.ProbeOllamaContext(ctx, http.DefaultClient, pc.BaseURL, cfg.DefaultModel); err == nil && n > 0 {
		llm.RememberContextLength(cfg.DefaultModel, n)
		return
	}
	// those servers advertise the window on /models (context_length,
	// max_model_len) or /models/status, which ProbeContextLength reads.
	if llm.ProbeContextLength(ctx, cfg.DefaultProvider, pc, cfg.DefaultModel) > 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "bee: context window unknown for %s; using fallback budget\n", cfg.DefaultModel)
}
