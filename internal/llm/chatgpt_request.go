package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/elhenro/bee/internal/llm/wire"
)

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
	// chatgpt.com codex backend filters models by originator; without this
	// header it rejects every modern model as "not supported when using
	// Codex with a ChatGPT account". Same value the Codex CLI sends.
	r.Header.Set("originator", "codex_cli_rs")

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
