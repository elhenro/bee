package llm

import (
	"cmp"
	"context"
	"net/http"
	"sync"
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
	cfg.Name = cmp.Or(cfg.Name, "chatgpt")
	return &ChatGPTProvider{cfg: cfg, client: cmp.Or(cfg.HTTPClient, newStreamingClient())}
}

// Name returns the configured display name.
func (p *ChatGPTProvider) Name() string { return p.cfg.Name }

// Stream issues a /responses call and emits Events on the returned channel.
func (p *ChatGPTProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	resp, err := p.do(ctx, req, false)
	if err != nil {
		return nil, err
	}

	return startStream(ctx, resp, req.Stream, p.streamLoop, p.nonStreamLoop), nil
}
