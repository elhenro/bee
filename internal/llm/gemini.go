package llm

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/elhenro/bee/internal/llm/wire"
	"github.com/elhenro/bee/internal/types"
)

// GeminiConfig configures the native Gemini provider. BaseURL defaults to
// Google's generative-language v1beta endpoint; APIKey is appended as a
// query param (Google's recommended auth path for this API).
type GeminiConfig struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// GeminiProvider streams from Google's generative-language API via
// :streamGenerateContent?alt=sse.
type GeminiProvider struct {
	cfg    GeminiConfig
	client *http.Client
}

// NewGemini builds a provider. APIKey is read from GEMINI_API_KEY env when
// the config field is blank, mirroring how callers wire other providers.
func NewGemini(cfg GeminiConfig) *GeminiProvider {
	cfg.BaseURL = cmp.Or(cfg.BaseURL, "https://generativelanguage.googleapis.com/v1beta")
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("GEMINI_API_KEY")
	}
	cfg.HTTPClient = cmp.Or(cfg.HTTPClient, newStreamingClient())
	return &GeminiProvider{cfg: cfg, client: cfg.HTTPClient}
}

// Name returns the static provider identifier.
func (p *GeminiProvider) Name() string { return "gemini" }

// Stream issues streamGenerateContent and emits Events on the returned channel.
// Caller must read until close; ctx cancellation closes the channel after an
// EventError. Gemini SSE: each line `data: <json>\n\n`; the model terminates
// by emitting `finishReason: "STOP"` (no separate `[DONE]` marker, but we
// handle one defensively).
func (p *GeminiProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	body, err := json.Marshal(buildGeminiRequest(req))
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/models/" + req.Model + ":streamGenerateContent?alt=sse"
	resp, err := postJSON(ctx, p.client, "gemini", url, body, func(r *http.Request) {
		setJSONHeaders(r, true)
		// key via header, never the URL query: a *url.Error from a transport failure
		// embeds the full URL (Go redacts userinfo, not query params), which would
		// leak the key into error output and transcripts.
		if p.cfg.APIKey != "" {
			r.Header.Set("x-goog-api-key", p.cfg.APIKey)
		}
	})
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, p.Name()); err != nil {
		return nil, err
	}

	ch := make(chan Event, 16)
	go p.streamLoop(ctx, resp, ch)
	return ch, nil
}

// streamLoop parses Gemini's SSE response. Each chunk may contain text deltas,
// function calls, and/or a terminal finishReason. We don't emit EventDone
// per-chunk — only once at end of stream — so usage from the final chunk is
// preserved.
func (p *GeminiProvider) streamLoop(ctx context.Context, resp *http.Response, out chan<- Event) {
	defer resp.Body.Close()
	defer close(out)

	stopReason := ""
	var usage *Usage
	if !readSSE(ctx, resp, p.Name(), effectiveStallTimeout(), out, func(payload []byte, _ func()) error {
		if string(payload) == "[DONE]" {
			return errSSEDone
		}
		var chunk wire.GeminiStreamChunk
		if err := json.Unmarshal(payload, &chunk); err != nil {
			// fail loudly: skipping a chunk would hide content
			return fmt.Errorf("decode chunk: %w", err)
		}
		for _, cand := range chunk.Candidates {
			for _, part := range cand.Content.Parts {
				if part.Text != "" {
					if err := send(ctx, out, Event{Type: EventTextDelta, Delta: part.Text}); err != nil {
						return err
					}
				}
				if fc := part.FunctionCall; fc != nil {
					ev := newToolUse("", fc.Name, fc.Args)
					ev.ToolUse.Signature = part.ThoughtSignature
					if err := send(ctx, out, ev); err != nil {
						return err
					}
				}
			}
			stopReason = cmp.Or(cand.FinishReason, stopReason)
		}
		if m := chunk.UsageMetadata; m != nil {
			usage = &Usage{
				InputTokens:  m.PromptTokenCount,
				OutputTokens: m.CandidatesTokenCount + m.ThoughtsTokenCount,
				CachedTokens: m.CachedContentTokenCount,
			}
		}
		return nil
	}) {
		return
	}
	emit(ctx, out, Event{Type: EventDone, StopReason: stopReason, Usage: usage})
}

// buildGeminiRequest translates bee's Request into Gemini's wire shape.
// System goes into systemInstruction, messages into contents[]. Roles
// collapse: user/tool → "user", assistant → "model".
func buildGeminiRequest(req Request) wire.GeminiRequest {
	out := wire.GeminiRequest{}

	if req.System != "" {
		out.SystemInstruction = &wire.GeminiContent{
			Parts: []wire.GeminiPart{{Text: req.System}},
		}
	}

	// functionResponse.name must match the function name, but a result only
	// carries the call id
	names := map[string]string{}
	for _, m := range req.Messages {
		for _, c := range m.Content {
			if c.Type == types.BlockToolUse && c.Use != nil {
				names[c.Use.ID] = c.Use.Name
			}
		}
	}

	for _, m := range req.Messages {
		gc := wire.GeminiContent{Role: geminiRole(m.Role)}
		for _, c := range m.Content {
			switch c.Type {
			case types.BlockText:
				if c.Text != "" {
					gc.Parts = append(gc.Parts, wire.GeminiPart{Text: c.Text})
				}
			case types.BlockImage:
				mt := c.MediaType
				if mt == "" {
					mt = "image/png"
				}
				gc.Parts = append(gc.Parts, wire.GeminiPart{
					InlineData: &wire.GeminiInlineData{
						MimeType: mt,
						Data:     base64.StdEncoding.EncodeToString(c.Data),
					},
				})
			case types.BlockToolUse:
				if c.Use == nil {
					continue
				}
				gc.Parts = append(gc.Parts, wire.GeminiPart{
					FunctionCall: &wire.GeminiFunctionCall{
						Name: c.Use.Name,
						Args: c.Use.Input,
					},
					ThoughtSignature: c.Use.Signature,
				})
			case types.BlockToolResult:
				if c.Result == nil {
					continue
				}
				name := names[c.Result.UseID]
				if name == "" {
					name = c.Result.UseID // call not in history (compacted away)
				}
				gc.Parts = append(gc.Parts, wire.GeminiPart{
					FunctionResponse: &wire.GeminiFunctionResponse{
						Name:     name,
						Response: map[string]any{"content": c.Result.Content},
					},
				})
			}
		}
		if len(gc.Parts) > 0 {
			out.Contents = append(out.Contents, gc)
		}
	}

	if len(req.Tools) > 0 {
		decls := make([]wire.GeminiFunctionDecl, 0, len(req.Tools))
		for _, t := range req.Tools {
			decls = append(decls, wire.GeminiFunctionDecl{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Schema,
			})
		}
		out.Tools = []wire.GeminiTool{{FunctionDeclarations: decls}}
	}

	gen := map[string]any{}
	// gemini 2.5 family supports thinkingConfig.thinkingBudget. omit when off.
	if b := geminiThinkingBudget(req.Model, ThinkingBudget(req.Thinking)); b > 0 {
		gen["thinkingConfig"] = map[string]any{"thinkingBudget": b}
	}
	if req.MaxTokens > 0 {
		gen["maxOutputTokens"] = req.MaxTokens
	}
	if req.Temperature != 0 {
		gen["temperature"] = req.Temperature
	}
	if len(gen) > 0 {
		out.GenerationConfig = gen
	}
	return out
}

// geminiThinkingBudget clamps the budget to the model's ceiling; an
// over-cap value is a 400.
func geminiThinkingBudget(model string, b int) int {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "flash"):
		return min(b, 24576)
	case strings.Contains(m, "pro"):
		return min(b, 32768)
	}
	return b
}

// geminiRole collapses bee's four roles into Gemini's two ("user"/"model").
// System never appears here — it's handled via systemInstruction. Tool-role
// messages carry functionResponse parts and ride as user role.
func geminiRole(r types.Role) string {
	if r == types.RoleAssistant {
		return "model"
	}
	return "user"
}
