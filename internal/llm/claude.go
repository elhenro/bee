// claude.go is the native Anthropic Messages provider. Auth is direct
// API-key only (ANTHROPIC_API_KEY via x-api-key header). bee identifies
// itself honestly as bee/0.1; impersonating first-party clients to access
// subscription pricing is out of scope.
package llm

import (
	"bufio"
	"bytes"
	"context"
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

// claudeAPIVersion is the required `anthropic-version` header value.
const claudeAPIVersion = "2023-06-01"

// claudeUserAgent identifies bee honestly to the Anthropic API.
const claudeUserAgent = "bee/0.1 (+https://github.com/elhenro/bee)"

// ClaudeConfig configures the provider. API-key only — the Bearer/OAuth
// subscription path was removed.
type ClaudeConfig struct {
	// Name shows up in logs and Provider.Name(). Defaults to "claude".
	Name string
	// BaseURL e.g. "https://api.anthropic.com/v1". The "/messages" path is
	// appended automatically.
	BaseURL string
	// EnvKey is the API-key env var (e.g. ANTHROPIC_API_KEY).
	EnvKey string
	// HTTPClient is overridable for tests. Defaults to a long-timeout client.
	HTTPClient *http.Client
	// ExtraHeaders are merged into every request.
	ExtraHeaders map[string]string
}

// ClaudeProvider implements Provider against Anthropic's native Messages API.
type ClaudeProvider struct {
	cfg    ClaudeConfig
	client *http.Client
}

// NewClaude builds a provider.
func NewClaude(cfg ClaudeConfig) *ClaudeProvider {
	if cfg.Name == "" {
		cfg.Name = "claude"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.anthropic.com/v1"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = newStreamingClient()
	}
	return &ClaudeProvider{cfg: cfg, client: client}
}

// Name returns the configured display name.
func (p *ClaudeProvider) Name() string { return p.cfg.Name }

// Stream issues a /messages call and emits Events on the returned channel.
func (p *ClaudeProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	resp, err := p.do(ctx, req)
	if err != nil {
		return nil, err
	}
	tools := p.toolAdverts(req)
	out := make(chan Event, 16)
	if req.Stream {
		go p.streamLoop(ctx, resp, out, tools)
	} else {
		go p.nonStreamLoop(resp, out, tools)
	}
	return out, nil
}

// toolAdverts is the per-call set of advertised tools the wire builder + the
// stream parser both need.
func (p *ClaudeProvider) toolAdverts(req Request) []wire.ToolAdvert {
	tools := make([]wire.ToolAdvert, 0, len(req.Tools))
	for _, t := range req.Tools {
		tools = append(tools, wire.ToolAdvert{
			Name: t.Name, Description: t.Description, Schema: t.Schema,
		})
	}
	return tools
}

func (p *ClaudeProvider) do(ctx context.Context, req Request) (*http.Response, error) {
	tools := p.toolAdverts(req)
	body, err := json.Marshal(wire.BuildAnthropicMessagesRequest(
		req.Model, req.System, req.Messages, tools,
		req.MaxTokens, req.Temperature, req.Stream,
		ThinkingBudget(req.Thinking),
	))
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic messages request: %w", err)
	}

	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	p.applyHeaders(httpReq, req.Stream)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("post: %w", err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("provider %s status %d: %s", p.cfg.Name, resp.StatusCode, string(raw))
	}
	return resp, nil
}

func (p *ClaudeProvider) applyHeaders(r *http.Request, streaming bool) {
	r.Header.Set("Content-Type", "application/json")
	if streaming {
		r.Header.Set("Accept", "text/event-stream")
	} else {
		r.Header.Set("Accept", "application/json")
	}
	r.Header.Set("anthropic-version", claudeAPIVersion)
	r.Header.Set("User-Agent", claudeUserAgent)
	if key := os.Getenv(p.cfg.EnvKey); key != "" {
		r.Header.Set("x-api-key", key)
	}
	for k, v := range p.cfg.ExtraHeaders {
		r.Header.Set(k, v)
	}
}

// nonStreamLoop decodes a single Messages response and emits the equivalent
// events. Used when req.Stream is false (rare for interactive use, common
// for one-shot scripts).
func (p *ClaudeProvider) nonStreamLoop(resp *http.Response, out chan<- Event, tools []wire.ToolAdvert) {
	defer resp.Body.Close()
	defer close(out)
	_ = tools // reserved for future tool-name validation

	var body struct {
		Content []struct {
			Type  string         `json:"type"`
			Text  string         `json:"text,omitempty"`
			ID    string         `json:"id,omitempty"`
			Name  string         `json:"name,omitempty"`
			Input map[string]any `json:"input,omitempty"`
		} `json:"content"`
		StopReason string                `json:"stop_reason"`
		Usage      *wire.AnthropicUsage  `json:"usage,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		out <- Event{Type: EventError, Err: fmt.Errorf("decode non-stream body: %w", err)}
		return
	}
	for _, c := range body.Content {
		switch c.Type {
		case "text":
			if c.Text != "" {
				out <- Event{Type: EventTextDelta, Delta: c.Text}
			}
		case "tool_use":
			id := c.ID
			if id == "" {
				id = "call_" + uuid.NewString()
			}
			input := c.Input
			if input == nil {
				input = map[string]any{}
			}
			out <- Event{Type: EventToolUse, ToolUse: &types.ToolUse{
				ID: id, Name: c.Name, Input: input,
			}}
		}
	}
	done := Event{Type: EventDone, StopReason: body.StopReason}
	if body.Usage != nil {
		done.Usage = &Usage{InputTokens: body.Usage.InputTokens, OutputTokens: body.Usage.OutputTokens}
	}
	out <- done
}

// streamLoop parses the Messages SSE stream. Tool-call inputs arrive as
// incremental JSON deltas keyed by block index; we accumulate them and emit
// one EventToolUse per tool_use block at content_block_stop time.
func (p *ClaudeProvider) streamLoop(ctx context.Context, resp *http.Response, out chan<- Event, tools []wire.ToolAdvert) {
	defer resp.Body.Close()
	defer close(out)
	_ = tools // reserved for future tool-name validation

	type pending struct {
		id   string
		name string
		args strings.Builder
	}
	// Block index -> pending tool_use. Anthropic numbers blocks sequentially
	// within a single message_stop.
	calls := map[int]*pending{}
	var order []int
	var usage *wire.AnthropicUsage
	stopReason := ""

	bumpActivity, stalled, cancelWatchdog := streamWatchdog(ctx, resp.Body)
	defer cancelWatchdog()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		bumpActivity()
		select {
		case <-ctx.Done():
			out <- Event{Type: EventError, Err: ctx.Err()}
			return
		case <-stalled:
			out <- Event{Type: EventError, Err: fmt.Errorf("provider %s stream stalled: no data for %s (try a different model)", p.cfg.Name, streamStallTimeout)}
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 || line[0] == ':' {
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		ev, err := wire.ParseAnthropicEvent(payload)
		if err != nil {
			out <- Event{Type: EventError, Err: err}
			return
		}
		if ev == nil {
			continue
		}

		switch ev.Type {
		case "message_start":
			if ev.Message != nil && ev.Message.Usage != nil {
				usage = ev.Message.Usage
			}
		case "content_block_start":
			if ev.ContentBlock == nil {
				continue
			}
			if ev.ContentBlock.Type == "tool_use" {
				calls[ev.Index] = &pending{
					id:   ev.ContentBlock.ID,
					name: ev.ContentBlock.Name,
				}
				order = append(order, ev.Index)
			}
		case "content_block_delta":
			if ev.Delta == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				if ev.Delta.Text != "" {
					out <- Event{Type: EventTextDelta, Delta: ev.Delta.Text}
				}
			case "input_json_delta":
				if c, ok := calls[ev.Index]; ok {
					c.args.WriteString(ev.Delta.PartialJSON)
				}
			case "thinking_delta":
				if ev.Delta.Thinking != "" {
					out <- Event{Type: EventThinkingDelta, Delta: ev.Delta.Thinking}
				}
			case "signature_delta":
				// Anthropic signs thinking blocks so they can round-trip
				// back to the model next turn. bee currently drops thinking
				// blocks on the wire (types.BlockThinking is display-only),
				// so the signature is parsed and discarded. Kept here so
				// future thinking-roundtrip work has a hook point.
			}
		case "message_delta":
			if ev.Delta != nil && ev.Delta.StopReason != "" {
				stopReason = ev.Delta.StopReason
			}
			if ev.Usage != nil {
				// usage on message_delta is the final tally; merge over the
				// message_start snapshot.
				if usage == nil {
					usage = ev.Usage
				} else {
					usage.OutputTokens = ev.Usage.OutputTokens
					if ev.Usage.InputTokens > 0 {
						usage.InputTokens = ev.Usage.InputTokens
					}
				}
			}
		case "error":
			out <- Event{Type: EventError, Err: fmt.Errorf("anthropic stream error: %s", string(payload))}
			return
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			out <- Event{Type: EventError, Err: ctx.Err()}
			return
		}
		out <- Event{Type: EventError, Err: fmt.Errorf("sse scan: %w", err)}
		return
	}

	for _, idx := range order {
		c := calls[idx]
		input := map[string]any{}
		if c.args.Len() > 0 {
			_ = json.Unmarshal([]byte(c.args.String()), &input)
		}
		id := c.id
		if id == "" {
			id = "call_" + uuid.NewString()
		}
		out <- Event{Type: EventToolUse, ToolUse: &types.ToolUse{
			ID: id, Name: c.name, Input: input,
		}}
	}

	final := Event{Type: EventDone, StopReason: stopReason}
	if usage != nil {
		final.Usage = &Usage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens}
	}
	out <- final
}
