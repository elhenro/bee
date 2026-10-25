// claude.go is the native Anthropic Messages provider. Auth is direct
// API-key only (ANTHROPIC_API_KEY via x-api-key header). bee identifies
// itself honestly as bee/0.1; impersonating first-party clients to access
// subscription pricing is out of scope.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/elhenro/bee/internal/llm/wire"
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
	out := make(chan Event, 16)
	if req.Stream {
		go p.streamLoop(ctx, resp, out)
	} else {
		go p.nonStreamLoop(resp, out)
	}
	return out, nil
}

func (p *ClaudeProvider) do(ctx context.Context, req Request) (*http.Response, error) {
	body, err := json.Marshal(wire.BuildAnthropicMessagesRequest(
		req.Model, req.System, req.SystemDynamic, req.Messages, toolAdverts(req.Tools),
		req.MaxTokens, req.Temperature, req.Stream,
		ThinkingBudget(req.Thinking),
	))
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic messages request: %w", err)
	}

	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/messages"
	resp, err := postJSON(ctx, p.client, p.cfg.Name, url, body, func(r *http.Request) { p.applyHeaders(r, req.Stream) })
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, p.cfg.Name); err != nil {
		return nil, err
	}
	return resp, nil
}

func (p *ClaudeProvider) applyHeaders(r *http.Request, streaming bool) {
	setJSONHeaders(r, streaming)
	r.Header.Set("anthropic-version", claudeAPIVersion)
	r.Header.Set("User-Agent", claudeUserAgent)
	// env first, then the key stored by /login (same order as config load)
	if key := os.Getenv(p.cfg.EnvKey); key != "" {
		r.Header.Set("x-api-key", key)
	} else if key := loadAuthAPIKey(p.cfg.Name); key != "" {
		r.Header.Set("x-api-key", key)
	}
	for k, v := range p.cfg.ExtraHeaders {
		r.Header.Set(k, v)
	}
}

// nonStreamLoop decodes a single Messages response and emits the equivalent
// events. Used when req.Stream is false (rare for interactive use, common
// for one-shot scripts).
func (p *ClaudeProvider) nonStreamLoop(resp *http.Response, out chan<- Event) {
	defer resp.Body.Close()
	defer close(out)

	var body struct {
		Content []struct {
			Type  string         `json:"type"`
			Text  string         `json:"text,omitempty"`
			ID    string         `json:"id,omitempty"`
			Name  string         `json:"name,omitempty"`
			Input map[string]any `json:"input,omitempty"`
		} `json:"content"`
		StopReason string               `json:"stop_reason"`
		Usage      *wire.AnthropicUsage `json:"usage,omitempty"`
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
			if c.Input == nil {
				c.Input = map[string]any{}
			}
			out <- newToolUse(c.ID, c.Name, c.Input)
		}
	}
	out <- Event{Type: EventDone, StopReason: body.StopReason, Usage: anthropicUsage(body.Usage)}
}
