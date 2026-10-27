package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/elhenro/bee/internal/llm/wire"
)

// do POSTs the request, transparently refreshing the token once on 401.
func (p *ChatGPTProvider) do(ctx context.Context, req Request, isRetry bool) (*http.Response, error) {
	body, err := json.Marshal(wire.BuildResponsesRequest(req.Model, req.System, req.Messages, toolAdverts(req.Tools), req.MaxTokens, req.Temperature, req.Stream, reasoningEffort(req.Thinking)))
	if err != nil {
		return nil, fmt.Errorf("marshal responses request: %w", err)
	}

	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/responses"
	// bearer the last attempt carried, so a refresh after 401 can tell
	// whether another caller already rotated the token.
	var sentBearer string
	resp, err := postJSON(ctx, p.client, p.cfg.Name, url, body, func(r *http.Request) {
		p.applyHeaders(r, req.Stream)
		sentBearer = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 401 && !isRetry && p.cfg.TokenEndpoint != "" {
		resp.Body.Close()
		if refreshed, rerr := p.refresh(ctx, sentBearer); rerr == nil && refreshed {
			return p.do(ctx, req, true)
		}
		// fall through with a clean error message
		return nil, fmt.Errorf("provider %s: 401 unauthorized and refresh failed; run /login %s", p.cfg.Name, p.cfg.Name)
	}
	if err := checkStatus(resp, p.cfg.Name); err != nil {
		return nil, err
	}
	return resp, nil
}

func (p *ChatGPTProvider) applyHeaders(r *http.Request, streaming bool) {
	setJSONHeaders(r, streaming)
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
}
