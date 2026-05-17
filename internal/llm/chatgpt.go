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

	"github.com/google/uuid"
	"github.com/elhenro/bee/internal/auth"
	"github.com/elhenro/bee/internal/llm/wire"
	"github.com/elhenro/bee/internal/types"
)

// ChatGPTConfig drives an OpenAI Responses-API provider with subscription
// auth. Use this for chatgpt.com/backend-api/codex (Plus/Pro/Team plans).
type ChatGPTConfig struct {
	// Name shows up in logs and Provider.Name(). Defaults to "chatgpt".
	Name string
	// BaseURL e.g. "https://chatgpt.com/backend-api/codex". Path "/responses"
	// is appended automatically.
	BaseURL string
	// ClientID is the OAuth client id used to refresh tokens on 401.
	ClientID string
	// TokenEndpoint is the OAuth token URL for refresh.
	TokenEndpoint string
	// AccountIDHeader is the HTTP header that carries the per-account id
	// (e.g. "chatgpt-account-id"). Empty disables injection.
	AccountIDHeader string
	// HTTPClient is overridable for tests. Defaults to a long-timeout client.
	HTTPClient *http.Client
	// ExtraHeaders are merged into every request.
	ExtraHeaders map[string]string
}

// ChatGPTProvider implements Provider against OpenAI's Responses API on the
// ChatGPT-subscription backend.
type ChatGPTProvider struct {
	cfg    ChatGPTConfig
	client *http.Client
	mu     sync.Mutex // guards refreshInFlight
}

// NewChatGPT builds a provider.
func NewChatGPT(cfg ChatGPTConfig) *ChatGPTProvider {
	if cfg.Name == "" {
		cfg.Name = "chatgpt"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = newStreamingClient()
	}
	return &ChatGPTProvider{cfg: cfg, client: client}
}

// Name returns the configured display name.
func (p *ChatGPTProvider) Name() string { return p.cfg.Name }

// Stream issues a /responses call and emits Events on the returned channel.
func (p *ChatGPTProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	resp, err := p.do(ctx, req, false)
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

// do POSTs the request, transparently refreshing the token once on 401.
func (p *ChatGPTProvider) do(ctx context.Context, req Request, isRetry bool) (*http.Response, error) {
	tools := make([]wire.ToolAdvert, 0, len(req.Tools))
	for _, t := range req.Tools {
		tools = append(tools, wire.ToolAdvert{
			Name: t.Name, Description: t.Description, Schema: t.Schema,
		})
	}
	effort := ""
	if req.Thinking != "" && req.Thinking != ThinkingOff {
		effort = string(req.Thinking)
		if req.Thinking == ThinkingMax {
			effort = string(ThinkingHigh)
		}
	}
	body, err := json.Marshal(wire.BuildResponsesRequest(req.Model, req.System, req.Messages, tools, req.MaxTokens, req.Temperature, req.Stream, effort))
	if err != nil {
		return nil, fmt.Errorf("marshal responses request: %w", err)
	}

	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/responses"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	p.applyHeaders(httpReq, req.Stream)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("post: %w", err)
	}
	if resp.StatusCode == 401 && !isRetry && p.cfg.TokenEndpoint != "" {
		resp.Body.Close()
		if refreshed, rerr := p.refresh(ctx); rerr == nil && refreshed {
			return p.do(ctx, req, true)
		}
		// fall through with a clean error message
		return nil, fmt.Errorf("provider %s: 401 unauthorized and refresh failed; run /login %s", p.cfg.Name, p.cfg.Name)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("provider %s status %d: %s", p.cfg.Name, resp.StatusCode, string(raw))
	}
	return resp, nil
}

func (p *ChatGPTProvider) applyHeaders(r *http.Request, streaming bool) {
	r.Header.Set("Content-Type", "application/json")
	if streaming {
		r.Header.Set("Accept", "text/event-stream")
	} else {
		r.Header.Set("Accept", "application/json")
	}
	r.Header.Set("User-Agent", "bee/0.1 (+https://github.com/elhenro/bee)")
	r.Header.Set("OpenAI-Beta", "responses=v1")

	tok := loadAuthToken(p.cfg.Name)
	if tok != nil && tok.AccessToken != "" {
		r.Header.Set("Authorization", "Bearer "+tok.AccessToken)
		if p.cfg.AccountIDHeader != "" && tok.AccountID != "" {
			r.Header.Set(p.cfg.AccountIDHeader, tok.AccountID)
		}
	}
	for k, v := range p.cfg.ExtraHeaders {
		r.Header.Set(k, v)
	}
}

// refresh swaps the stored refresh_token for a fresh access_token. Returns
// (true, nil) on success. Concurrent callers serialize on p.mu so we only
// refresh once even if multiple requests race a 401.
func (p *ChatGPTProvider) refresh(ctx context.Context) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	dir := filepath.Join(home, ".bee", "auth")
	tok, err := auth.LoadToken(dir, p.cfg.Name)
	if err != nil || tok == nil {
		return false, fmt.Errorf("no token to refresh")
	}
	if tok.RefreshToken == "" {
		return false, fmt.Errorf("token has no refresh_token")
	}
	newTok, err := auth.RefreshToken(ctx, p.cfg.TokenEndpoint, p.cfg.ClientID, tok.RefreshToken)
	if err != nil {
		return false, err
	}
	// Preserve fields the refresh response usually omits.
	if newTok.RefreshToken == "" {
		newTok.RefreshToken = tok.RefreshToken
	}
	if newTok.IDToken == "" {
		newTok.IDToken = tok.IDToken
	}
	if newTok.AccountID == "" {
		newTok.AccountID = tok.AccountID
	}
	if err := auth.SaveToken(dir, p.cfg.Name, newTok); err != nil {
		return false, err
	}
	return true, nil
}

// loadAuthToken is the full-token variant of loadAuthBearer in openai_compat.go.
func loadAuthToken(provider string) *auth.Token {
	if provider == "" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	tok, err := auth.LoadToken(filepath.Join(home, ".bee", "auth"), provider)
	if err != nil {
		return nil
	}
	return tok
}

// nonStreamLoop reads a single JSON body and emits the equivalent events.
func (p *ChatGPTProvider) nonStreamLoop(resp *http.Response, out chan<- Event) {
	defer resp.Body.Close()
	defer close(out)

	var body wire.ResponsesEventBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		out <- Event{Type: EventError, Err: fmt.Errorf("decode non-stream body: %w", err)}
		return
	}
	for _, item := range body.Output {
		switch item.Type {
		case "message":
			for _, c := range item.Content {
				if c.Type == "output_text" && c.Text != "" {
					out <- Event{Type: EventTextDelta, Delta: c.Text}
				}
			}
		case "function_call":
			input := map[string]any{}
			if item.Arguments != "" {
				_ = json.Unmarshal([]byte(item.Arguments), &input)
			}
			id := item.CallID
			if id == "" {
				id = "call_" + uuid.NewString()
			}
			out <- Event{Type: EventToolUse, ToolUse: &types.ToolUse{
				ID: id, Name: item.Name, Input: input,
			}}
		}
	}
	done := Event{Type: EventDone, StopReason: body.Status}
	if body.Usage != nil {
		done.Usage = &Usage{InputTokens: body.Usage.InputTokens, OutputTokens: body.Usage.OutputTokens}
	}
	out <- done
}

// streamLoop parses the Responses SSE stream. Tool-call arguments arrive as
// incremental deltas; accumulate per-call and emit one EventToolUse on done.
func (p *ChatGPTProvider) streamLoop(ctx context.Context, resp *http.Response, out chan<- Event) {
	defer resp.Body.Close()
	defer close(out)

	// per-item state keyed by item_id (also call_id for function_call items)
	type pending struct {
		callID string
		name   string
		args   strings.Builder
	}
	calls := map[string]*pending{}
	var order []string
	var usage *wire.ResponsesUsage
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
		// Responses API emits both `event:` and `data:` lines. We only need
		// the data payload.
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		ev, err := wire.ParseResponsesEvent(payload)
		if err != nil {
			out <- Event{Type: EventError, Err: err}
			return
		}
		if ev == nil {
			continue
		}

		switch ev.Type {
		case "response.output_text.delta":
			if ev.Delta != "" {
				out <- Event{Type: EventTextDelta, Delta: ev.Delta}
			}
		case "response.output_item.added":
			if ev.Item != nil && ev.Item.Type == "function_call" {
				id := ev.Item.ID
				if id == "" {
					id = ev.Item.CallID
				}
				if id == "" {
					continue
				}
				if _, ok := calls[id]; !ok {
					calls[id] = &pending{callID: ev.Item.CallID, name: ev.Item.Name}
					order = append(order, id)
				}
				if calls[id].callID == "" {
					calls[id].callID = ev.Item.CallID
				}
				if calls[id].name == "" {
					calls[id].name = ev.Item.Name
				}
			}
		case "response.function_call_arguments.delta":
			id := ev.ItemID
			if id == "" {
				continue
			}
			c, ok := calls[id]
			if !ok {
				c = &pending{}
				calls[id] = c
				order = append(order, id)
			}
			c.args.WriteString(ev.Delta)
		case "response.function_call_arguments.done":
			id := ev.ItemID
			c, ok := calls[id]
			if !ok {
				continue
			}
			if ev.Arguments != "" {
				// authoritative final form; replace accumulator
				c.args.Reset()
				c.args.WriteString(ev.Arguments)
			}
		case "response.output_item.done":
			if ev.Item != nil && ev.Item.Type == "function_call" {
				id := ev.Item.ID
				if id == "" {
					id = ev.Item.CallID
				}
				if c, ok := calls[id]; ok {
					if c.callID == "" {
						c.callID = ev.Item.CallID
					}
					if c.name == "" {
						c.name = ev.Item.Name
					}
					if ev.Item.Arguments != "" {
						c.args.Reset()
						c.args.WriteString(ev.Item.Arguments)
					}
				}
			}
		case "response.completed":
			if ev.Response != nil {
				if ev.Response.Usage != nil {
					usage = ev.Response.Usage
				}
				if ev.Response.Status != "" {
					stopReason = ev.Response.Status
				}
			}
		case "response.failed", "response.error":
			msg := "responses stream failed"
			if ev.Response != nil && ev.Response.Error != nil {
				msg = ev.Response.Error.Message
				if msg == "" {
					msg = ev.Response.Error.Code
				}
			}
			out <- Event{Type: EventError, Err: fmt.Errorf("%s", msg)}
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

	for _, id := range order {
		c := calls[id]
		input := map[string]any{}
		if c.args.Len() > 0 {
			_ = json.Unmarshal([]byte(c.args.String()), &input)
		}
		callID := c.callID
		if callID == "" {
			callID = "call_" + uuid.NewString()
		}
		out <- Event{Type: EventToolUse, ToolUse: &types.ToolUse{
			ID: callID, Name: c.name, Input: input,
		}}
	}

	final := Event{Type: EventDone, StopReason: stopReason}
	if usage != nil {
		final.Usage = &Usage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens}
	}
	out <- final
}
