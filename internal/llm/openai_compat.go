package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/elhenro/bee/internal/llm/wire"
)

// OpenAICompatConfig configures a chat-completions-style provider. The same
// implementation covers OpenRouter, OpenAI, DeepSeek, Groq, Ollama, LM Studio,
// Together, Fireworks — any service that speaks OpenAI's wire format.
type OpenAICompatConfig struct {
	// Name shows up in logs and Provider.Name(). Defaults to "openai-compat".
	Name string
	// BaseURL e.g. "https://openrouter.ai/api/v1". The "/chat/completions"
	// suffix is appended automatically.
	BaseURL string
	// EnvKey is the environment variable holding the bearer token. Optional —
	// when blank (local servers like Ollama), no Authorization header is sent.
	EnvKey string
	// HTTPClient is overridable for tests. Defaults to a long-timeout client.
	HTTPClient *http.Client
	// ExtraHeaders are merged into every request. Useful for OpenRouter's
	// HTTP-Referer + X-Title attribution headers.
	ExtraHeaders map[string]string
	// StallTimeout overrides the per-chunk SSE inactivity timer. 0 falls back
	// to the package default (streamStallTimeout). Negative disables the
	// watchdog (test-only).
	StallTimeout time.Duration
}

// OpenAICompatProvider implements Provider against an OpenAI-compatible API.
type OpenAICompatProvider struct {
	cfg    OpenAICompatConfig
	client *http.Client
	// noTools caches models the provider has refused tools for (e.g. Ollama
	// returns 400 "does not support tools" for non-tool-tuned models). Once
	// observed, future requests for that model skip tools without paying the
	// failed-attempt round-trip.
	noTools sync.Map // model string -> struct{}
}

// NewOpenAICompat builds a provider. Missing fields get sensible defaults.
func NewOpenAICompat(cfg OpenAICompatConfig) *OpenAICompatProvider {
	if cfg.Name == "" {
		cfg.Name = "openai-compat"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = newStreamingClient()
	}
	return &OpenAICompatProvider{cfg: cfg, client: client}
}

// retry policy for the pre-stream request: network errors and 408/429/5xx
// trigger an exp-backoff retry. Once the stream starts emitting deltas we
// no longer retry — replaying would duplicate tokens.
const (
	maxRetryAttempts = 3
	retryBaseDelay   = 800 * time.Millisecond
)

// retryableStatus reports whether a server response code is worth retrying.
// 408 request timeout, 429 too many requests, 5xx server errors — same set
// the OpenAI/Anthropic SDKs use.
func retryableStatus(code int) bool {
	switch code {
	case 408, 429, 500, 502, 503, 504:
		return true
	}
	return false
}

// isNoToolSupportError detects the Ollama/llama.cpp 400 returned for models
// that lack tool calling. Matched on substring so it survives Ollama version
// drift; the model id is interpolated into the message so we anchor on the
// suffix instead.
func isNoToolSupportError(code int, body []byte) bool {
	if code != 400 {
		return false
	}
	s := strings.ToLower(string(body))
	return strings.Contains(s, "does not support tools") ||
		strings.Contains(s, "model does not support tool")
}

// Name returns the configured display name.
func (p *OpenAICompatProvider) Name() string { return p.cfg.Name }

// Stream issues a chat completion and emits Events on the returned channel.
// Caller must read the channel until closed to avoid leaking the goroutine;
// ctx cancellation closes the channel after an EventError.
//
// Retries the pre-stream phase (DNS/TCP/TLS failures, 408, 429, 5xx) with
// exp-backoff. Once a 2xx response lands and streamLoop starts emitting, no
// further retries — replaying would duplicate tokens.
func (p *OpenAICompatProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	wireReq := buildWireRequest(req)
	if _, banned := p.noTools.Load(req.Model); banned {
		wireReq.Tools = nil
	}
	body, err := json.Marshal(wireReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/chat/completions"

	var (
		resp    *http.Response
		lastErr error
	)
	for attempt := 0; attempt < maxRetryAttempts; attempt++ {
		if attempt > 0 {
			delay := retryBaseDelay << (attempt - 1) // 800ms, 1.6s, 3.2s
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		httpReq, rerr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if rerr != nil {
			return nil, fmt.Errorf("build request: %w", rerr)
		}
		p.applyHeaders(httpReq, req.Stream)

		r, derr := p.client.Do(httpReq)
		if derr != nil {
			// don't retry context cancellation
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = fmt.Errorf("post: %w", derr)
			continue
		}

		if r.StatusCode >= 400 {
			raw, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
			_ = r.Body.Close()
			// Ollama (and some llama.cpp wrappers) reject tool advertisements
			// for non-tool-tuned models with 400 "does not support tools".
			// Strip tools, remember the model, rebuild body, retry once.
			if len(wireReq.Tools) > 0 && isNoToolSupportError(r.StatusCode, raw) {
				p.noTools.Store(req.Model, struct{}{})
				wireReq.Tools = nil
				nb, merr := json.Marshal(wireReq)
				if merr != nil {
					return nil, fmt.Errorf("marshal request: %w", merr)
				}
				body = nb
				lastErr = fmt.Errorf("provider %s status %d: %s", p.cfg.Name, r.StatusCode, string(raw))
				continue
			}
			if retryableStatus(r.StatusCode) {
				lastErr = fmt.Errorf("provider %s status %d: %s", p.cfg.Name, r.StatusCode, string(raw))
				continue
			}
			return nil, fmt.Errorf("provider %s status %d: %s", p.cfg.Name, r.StatusCode, string(raw))
		}

		resp = r
		break
	}
	if resp == nil {
		return nil, fmt.Errorf("provider %s exhausted retries: %w", p.cfg.Name, lastErr)
	}

	out := make(chan Event, 16)
	if req.Stream {
		go p.streamLoop(ctx, resp, out)
	} else {
		go p.nonStreamLoop(resp, out)
	}
	return out, nil
}

func buildWireRequest(req Request) wire.ChatRequest {
	tools := make([]wire.ToolAdvert, 0, len(req.Tools))
	for _, t := range req.Tools {
		tools = append(tools, wire.ToolAdvert{
			Name: t.Name, Description: t.Description, Schema: t.Schema,
		})
	}
	wr := wire.BuildRequest(req.Model, req.System, req.Messages, tools, req.MaxTokens, req.Temperature, req.Stream)
	// OpenAI o-series + compatible: pass thinking level as reasoning_effort.
	// Omit on Off so non-reasoning models don't choke on the unknown field.
	// "max" isn't an OpenAI tier — clamp to "high" on the wire.
	if req.Thinking != "" && req.Thinking != ThinkingOff {
		eff := string(req.Thinking)
		if req.Thinking == ThinkingMax {
			eff = string(ThinkingHigh)
		}
		wr.ReasoningEffort = eff
	}
	return wr
}

// TODO: integration test against real OpenRouter, gated by BEE_E2E=1.
