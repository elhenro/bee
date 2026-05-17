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
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/elhenro/bee/internal/auth"
	"github.com/elhenro/bee/internal/llm/wire"
	"github.com/elhenro/bee/internal/types"
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

func (p *OpenAICompatProvider) applyHeaders(r *http.Request, streaming bool) {
	r.Header.Set("Content-Type", "application/json")
	if streaming {
		r.Header.Set("Accept", "text/event-stream")
	} else {
		r.Header.Set("Accept", "application/json")
	}
	r.Header.Set("HTTP-Referer", "https://github.com/elhenro/bee")
	r.Header.Set("X-Title", "bee")
	// Auth resolution order:
	//   1. OAuth bearer token saved by /login
	//   2. static api key saved by /login (apikey.SaveAPIKey)
	//   3. environment variable named by EnvKey
	// (1) and (2) live in ~/.bee/auth/ as <provider>.json and <provider>.key
	// respectively. Without (2) the static api key path is dead storage and
	// the user sees 401 even after "✓ key saved".
	if tok := loadAuthBearer(p.cfg.Name); tok != "" {
		r.Header.Set("Authorization", "Bearer "+tok)
	} else if key := loadAuthAPIKey(p.cfg.Name); key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	} else if p.cfg.EnvKey != "" {
		if key := os.Getenv(p.cfg.EnvKey); key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
	}
	for k, v := range p.cfg.ExtraHeaders {
		r.Header.Set(k, v)
	}
}

// loadAuthBearer returns the access_token saved by /login for provider, or
// "" if no token file exists or it's unreadable. Expired tokens are still
// returned (the API will respond 401 and the user re-runs /login).
func loadAuthBearer(provider string) string {
	if provider == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	tok, err := auth.LoadToken(filepath.Join(home, ".bee", "auth"), provider)
	if err != nil || tok == nil {
		return ""
	}
	return tok.AccessToken
}

// loadAuthAPIKey returns the static api key saved by /login (the api-key
// sub-mode in LoginPane) for provider, or "" if absent. Mirrors
// loadAuthBearer's lookup so static-key providers (omlx, deepseek, groq, …)
// pick up the same ~/.bee/auth/<provider>.key file the login pane writes.
func loadAuthAPIKey(provider string) string {
	if provider == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	key, err := auth.LoadAPIKey(filepath.Join(home, ".bee", "auth"), provider)
	if err != nil {
		return ""
	}
	return key
}

// nonStreamLoop drains a single JSON body and emits the equivalent events.
func (p *OpenAICompatProvider) nonStreamLoop(resp *http.Response, out chan<- Event) {
	defer resp.Body.Close()
	defer close(out)

	var body struct {
		Choices []struct {
			Message struct {
				Content   string             `json:"content"`
				ToolCalls []wire.ToolCall    `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *wire.StreamUsage `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		out <- Event{Type: EventError, Err: fmt.Errorf("decode non-stream body: %w", err)}
		return
	}
	if len(body.Choices) == 0 {
		out <- Event{Type: EventDone, StopReason: "stop"}
		return
	}
	choice := body.Choices[0]
	if choice.Message.Content != "" {
		out <- Event{Type: EventTextDelta, Delta: choice.Message.Content}
	}
	for _, tc := range choice.Message.ToolCalls {
		input := map[string]any{}
		var parseErr string
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
				parseErr = err.Error()
				input = map[string]any{}
			}
		}
		if parseErr != "" {
			input = map[string]any{"_parse_error": parseErr, "_raw_args": tc.Function.Arguments}
		}
		id := tc.ID
		if id == "" {
			id = "call_" + uuid.NewString()
		}
		out <- Event{Type: EventToolUse, ToolUse: &types.ToolUse{
			ID: id, Name: tc.Function.Name, Input: input,
		}}
	}
	done := Event{Type: EventDone, StopReason: choice.FinishReason}
	if body.Usage != nil {
		done.Usage = &Usage{InputTokens: body.Usage.PromptTokens, OutputTokens: body.Usage.CompletionTokens}
	}
	out <- done
}

// streamLoop parses an SSE stream, threading tool-call deltas through an
// accumulator and emitting per-chunk events.
func (p *OpenAICompatProvider) streamLoop(ctx context.Context, resp *http.Response, out chan<- Event) {
	defer resp.Body.Close()
	defer close(out)

	acc := wire.NewToolCallAccumulator()
	stopReason := ""
	var usage *wire.StreamUsage

	// inactivity watchdog: cancels the request when no SSE line arrives for
	// the configured stall window. Without this, scanner.Scan() blocks
	// indefinitely on a live-but-quiet connection while the TUI loader spins.
	stallWindow := p.cfg.StallTimeout
	if stallWindow == 0 {
		stallWindow = streamStallTimeout
	}
	bumpActivity, stalled, cancelWatchdog := streamWatchdogWith(ctx, resp.Body, stallWindow)
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
			out <- Event{Type: EventError, Err: fmt.Errorf("provider %s sse stalled: no data for %s (try a different model)", p.cfg.Name, stallWindow)}
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
		payload := bytes.TrimPrefix(line, []byte("data:"))

		chunk, done, err := wire.ParseChunk(payload)
		if err != nil {
			out <- Event{Type: EventError, Err: err}
			return
		}
		if done {
			break
		}
		if chunk == nil {
			continue
		}

		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		for _, ch := range chunk.Choices {
			// reasoning_content / reasoning arrive on the delta for DeepSeek-
			// reasoner and OpenAI-compat reasoning models. Surface as a
			// separate event so the renderer can show thoughts grayed
			// without mixing them into assistant content.
			if ch.Delta.ReasoningContent != "" {
				out <- Event{Type: EventThinkingDelta, Delta: ch.Delta.ReasoningContent}
			}
			if ch.Delta.Reasoning != "" {
				out <- Event{Type: EventThinkingDelta, Delta: ch.Delta.Reasoning}
			}
			if ch.Delta.Content != "" {
				out <- Event{Type: EventTextDelta, Delta: ch.Delta.Content}
			}
			if len(ch.Delta.ToolCalls) > 0 {
				acc.Apply(ch.Delta.ToolCalls)
			}
			if ch.FinishReason != nil && *ch.FinishReason != "" {
				stopReason = *ch.FinishReason
			}
		}
	}

	if err := scanner.Err(); err != nil {
		// surface ctx cancellation cleanly
		if ctx.Err() != nil {
			out <- Event{Type: EventError, Err: ctx.Err()}
			return
		}
		out <- Event{Type: EventError, Err: fmt.Errorf("sse scan: %w", err)}
		return
	}

	calls, err := acc.Finalize()
	if err != nil {
		out <- Event{Type: EventError, Err: err}
		return
	}
	for _, c := range calls {
		id := c.ID
		if id == "" {
			id = "call_" + uuid.NewString()
		}
		input := c.Input
		// parse failure: stop silently swallowing — pass a structured error
		// down so the tool layer surfaces a real diagnostic to the model on
		// the next turn instead of acting on empty input.
		if c.ParseError != "" && len(input) == 0 {
			input = map[string]any{"_parse_error": c.ParseError, "_raw_args": c.RawArgs}
		}
		out <- Event{Type: EventToolUse, ToolUse: &types.ToolUse{
			ID: id, Name: c.Name, Input: input,
		}}
	}
	final := Event{Type: EventDone, StopReason: stopReason}
	if usage != nil {
		final.Usage = &Usage{InputTokens: usage.PromptTokens, OutputTokens: usage.CompletionTokens}
	}
	out <- final
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
	if req.Thinking != "" && req.Thinking != ThinkingOff {
		wr.ReasoningEffort = string(req.Thinking)
	}
	return wr
}

// TODO: integration test against real OpenRouter, gated by BEE_E2E=1.
