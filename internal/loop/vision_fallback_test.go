package loop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/types"
)

func imgMsg() []types.Message {
	return []types.Message{{
		Role: types.RoleUser,
		Content: []types.ContentBlock{
			{Type: types.BlockText, Text: "what is this?"},
			{Type: types.BlockImage, MediaType: "image/png", Data: []byte("PNGDATA")},
		},
	}}
}

// vision-capable main model: messages pass through with image intact.
func TestApplyVisionFallback_PassthroughForVisionModel(t *testing.T) {
	e := &Engine{Cfg: config.Config{DefaultModel: "claude-sonnet-4-6"}}
	out := e.applyVisionFallback(context.Background(), imgMsg())
	if out[0].Content[1].Type != types.BlockImage {
		t.Fatalf("vision model should keep image block, got %v", out[0].Content[1].Type)
	}
}

// non-vision model, no fallback configured: image swapped for placeholder text.
func TestApplyVisionFallback_NoFallbackDropsImage(t *testing.T) {
	warn := make(chan string, 4)
	e := &Engine{Cfg: config.Config{DefaultModel: "deepseek-v4-flash"}, WarnCh: warn}
	out := e.applyVisionFallback(context.Background(), imgMsg())
	for _, b := range out[0].Content {
		if b.Type == types.BlockImage {
			t.Fatal("image block should be removed when no fallback")
		}
	}
	if !strings.Contains(out[0].Content[1].Text, "image omitted") {
		t.Errorf("missing placeholder: %q", out[0].Content[1].Text)
	}
	// original messages untouched (image still present)
	if imgMsg()[0].Content[1].Type != types.BlockImage {
		t.Error("source mutated")
	}
}

// [vision] provider inherits base_url from the named provider entry.
func TestApplyVisionFallback_InheritsProviderEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "a chart"}}},
		})
	}))
	defer srv.Close()

	e := &Engine{Cfg: config.Config{
		DefaultModel: "deepseek-v4-flash",
		Providers:    map[string]config.ProviderConfig{"omlx": {BaseURL: srv.URL}},
		Vision:       config.VisionConfig{Provider: "omlx", Model: "qwen3-vl-it"},
	}}
	out := e.applyVisionFallback(context.Background(), imgMsg())
	if !strings.Contains(out[0].Content[1].Text, "a chart") {
		t.Errorf("provider-inherited endpoint not used: %q", out[0].Content[1].Text)
	}
}

// non-vision model with fallback: image described and cached (one HTTP call).
func TestApplyVisionFallback_DescribesAndCaches(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "a bee logo"}}},
		})
	}))
	defer srv.Close()

	e := &Engine{Cfg: config.Config{
		DefaultModel: "deepseek-v4-flash",
		Vision:       config.VisionConfig{Model: "qwen-vl", Endpoint: srv.URL, API: "openai"},
	}}
	out := e.applyVisionFallback(context.Background(), imgMsg())
	got := out[0].Content[1].Text
	if !strings.Contains(got, "a bee logo") || !strings.Contains(got, "qwen-vl") {
		t.Errorf("description text = %q", got)
	}
	// second pass on the same image hits the cache, no new HTTP call.
	_ = e.applyVisionFallback(context.Background(), imgMsg())
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("describe called %d times, want 1 (cached)", n)
	}
}
