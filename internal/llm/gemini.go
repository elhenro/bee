package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
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
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	if cfg.APIKey == "" {
		cfg.APIKey = envGeminiKey()
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = newStreamingClient()
	}
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
	if p.cfg.APIKey != "" {
		url += "&key=" + p.cfg.APIKey
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("post: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("gemini %d: %s", resp.StatusCode, string(raw))
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

	bumpActivity, stalled, cancelWatchdog := streamWatchdog(ctx, resp.Body)
	defer cancelWatchdog()

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)

	stopReason := ""
	var usage *Usage

	for sc.Scan() {
		bumpActivity()
		select {
		case <-ctx.Done():
			out <- Event{Type: EventError, Err: ctx.Err()}
			return
		case <-stalled:
			out <- Event{Type: EventError, Err: fmt.Errorf("gemini stream stalled: no data for %s (try a different model)", streamStallTimeout)}
			return
		default:
		}

		line := sc.Text()
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			break
		}

		var chunk wire.GeminiStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			// non-fatal: log via error event for visibility but keep stream alive
			// would mask other content; better to fail loudly here.
			out <- Event{Type: EventError, Err: fmt.Errorf("decode chunk: %w", err)}
			return
		}

		for _, cand := range chunk.Candidates {
			for _, part := range cand.Content.Parts {
				if part.Text != "" {
					out <- Event{Type: EventTextDelta, Delta: part.Text}
				}
				if part.FunctionCall != nil {
					out <- Event{Type: EventToolUse, ToolUse: &types.ToolUse{
						ID:    "call_" + uuid.NewString(),
						Name:  part.FunctionCall.Name,
						Input: part.FunctionCall.Args,
					}}
				}
			}
			if cand.FinishReason != "" {
				stopReason = cand.FinishReason
			}
		}
		if chunk.UsageMetadata != nil {
			usage = &Usage{
				InputTokens:  chunk.UsageMetadata.PromptTokenCount,
				OutputTokens: chunk.UsageMetadata.CandidatesTokenCount,
			}
		}
	}

	if err := sc.Err(); err != nil {
		if ctx.Err() != nil {
			out <- Event{Type: EventError, Err: ctx.Err()}
			return
		}
		out <- Event{Type: EventError, Err: fmt.Errorf("sse scan: %w", err)}
		return
	}

	done := Event{Type: EventDone, StopReason: stopReason}
	if usage != nil {
		done.Usage = usage
	}
	out <- done
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
				})
			case types.BlockToolResult:
				if c.Result == nil {
					continue
				}
				// gemini needs the tool name, not the call id. ToolResult only
				// has the use-id, so we pass it through — caller upstream can
				// preserve name->id mapping if strict matching is required.
				gc.Parts = append(gc.Parts, wire.GeminiPart{
					FunctionResponse: &wire.GeminiFunctionResponse{
						Name:     c.Result.UseID,
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

	// gemini 2.5 family supports thinkingConfig.thinkingBudget. omit when off.
	if b := ThinkingBudget(req.Thinking); b > 0 {
		if out.GenerationConfig == nil {
			out.GenerationConfig = map[string]any{}
		}
		out.GenerationConfig["thinkingConfig"] = map[string]any{"thinkingBudget": b}
	}

	if req.MaxTokens > 0 {
		if out.GenerationConfig == nil {
			out.GenerationConfig = map[string]any{}
		}
		out.GenerationConfig["maxOutputTokens"] = req.MaxTokens
	}
	if req.Temperature != 0 {
		if out.GenerationConfig == nil {
			out.GenerationConfig = map[string]any{}
		}
		out.GenerationConfig["temperature"] = req.Temperature
	}

	return out
}

// geminiRole collapses bee's four roles into Gemini's two ("user"/"model").
// System never appears here — it's handled via systemInstruction. Tool-role
// messages carry functionResponse parts and ride as user role.
func geminiRole(r types.Role) string {
	switch r {
	case types.RoleUser, types.RoleTool:
		return "user"
	case types.RoleAssistant:
		return "model"
	}
	return "user"
}

// envGeminiKey returns the API key from env if the config didn't set it.
func envGeminiKey() string { return os.Getenv("GEMINI_API_KEY") }
