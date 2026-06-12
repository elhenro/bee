package loop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
	"github.com/elhenro/bee/internal/vision"
)

// applyVisionFallback returns a wire-safe copy of msgs for the main model. When
// the model accepts images (or there are none) msgs pass through untouched. When
// it can't see, image blocks are swapped for text: either a description from the
// configured fallback vision model, or a placeholder + one-time hint if none is
// configured. Original msgs (with real image bytes) stay intact for the session.
func (e *Engine) applyVisionFallback(ctx context.Context, msgs []types.Message) []types.Message {
	if llm.SupportsVision(e.Cfg.DefaultModel) || !hasImageBlocks(msgs) {
		return msgs
	}
	client, ok := e.visionClient()
	if !ok {
		if !e.run.visionWarned {
			e.run.visionWarned = true
			e.warnf("model %q has no vision and no [vision] fallback set — image dropped. run /vision <model> or set [vision] in config", e.Cfg.DefaultModel)
		}
		return swapImages(msgs, func(types.ContentBlock) string {
			return "[image omitted: active model has no vision; configure a fallback vision model]"
		})
	}
	// e.run.visionCache is allocated (or borrowed from the previous Run) by
	// freshRunState, so the lazy-init guard that used to live here is gone.
	return swapImages(msgs, func(b types.ContentBlock) string {
		return e.describeCached(ctx, client, b)
	})
}

// visionClient builds the fallback client from cfg, inheriting endpoint + key
// from a named provider when [vision] provider is set. ok is false when no
// usable model+endpoint resolves.
func (e *Engine) visionClient() (vision.Client, bool) {
	v := e.Cfg.Vision
	if v.Model == "" {
		return vision.Client{}, false
	}
	endpoint, key := v.Endpoint, ""
	if v.EnvKey != "" {
		key = os.Getenv(v.EnvKey)
	}
	if v.Provider != "" {
		if pc, ok := e.Cfg.Providers[v.Provider]; ok {
			if endpoint == "" {
				endpoint = pc.BaseURL
			}
			if v.EnvKey == "" && pc.EnvKey != "" {
				key = os.Getenv(pc.EnvKey)
			}
		}
	}
	if endpoint == "" {
		return vision.Client{}, false
	}
	return vision.Client{Model: v.Model, Endpoint: endpoint, APIKey: key, API: v.API}, true
}

// describeCached returns the description for one image block, keyed by content
// hash so repeat turns and duplicate images cost nothing.
func (e *Engine) describeCached(ctx context.Context, c vision.Client, b types.ContentBlock) string {
	sum := sha256.Sum256(b.Data)
	hkey := hex.EncodeToString(sum[:])
	if d, ok := e.run.visionCache[hkey]; ok {
		return d
	}
	txt, err := c.Describe(ctx, b.Data, b.MediaType, "")
	if err != nil {
		e.warnf("vision fallback failed: %v", err)
		txt = "[image: vision fallback failed: " + err.Error() + "]"
	} else {
		txt = "[image description (via " + c.Model + "): " + txt + "]"
	}
	e.run.visionCache[hkey] = txt
	return txt
}

func hasImageBlocks(msgs []types.Message) bool {
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Type == types.BlockImage {
				return true
			}
		}
	}
	return false
}

// swapImages returns a copy of msgs with every image block replaced by a text
// block whose text comes from repl. Messages without images share the original
// content slice (no needless copy).
func swapImages(msgs []types.Message, repl func(types.ContentBlock) string) []types.Message {
	out := make([]types.Message, len(msgs))
	for i, m := range msgs {
		out[i] = m
		if !msgHasImage(m) {
			continue
		}
		nc := make([]types.ContentBlock, 0, len(m.Content))
		for _, b := range m.Content {
			if b.Type == types.BlockImage {
				nc = append(nc, types.ContentBlock{Type: types.BlockText, Text: repl(b)})
				continue
			}
			nc = append(nc, b)
		}
		out[i].Content = nc
	}
	return out
}

func msgHasImage(m types.Message) bool {
	for _, b := range m.Content {
		if b.Type == types.BlockImage {
			return true
		}
	}
	return false
}
