package llm

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"time"
)

// prewarmTimeout bounds the warm-up request; it runs detached from any turn.
const prewarmTimeout = 8 * time.Second

// Prewarmer is implemented by hosted providers that can open their connection
// before the first request. The dial, TLS handshake and HTTP/2 setup otherwise
// land inside the first turn's time-to-first-token.
type Prewarmer interface {
	Prewarm(ctx context.Context)
}

// prewarmConn warms the provider client's pool for baseURL. Loopback targets are
// skipped: a local server has no handshake worth hiding, and a test server would
// see a request it never asked for.
func prewarmConn(ctx context.Context, c *http.Client, baseURL string) {
	if c == nil || isLoopbackURL(baseURL) {
		return
	}
	warm(ctx, c, baseURL)
}

// warm sends one HEAD with no credentials. The response is irrelevant (an API
// root usually answers 404); the point is the pooled connection it leaves.
func warm(ctx context.Context, c *http.Client, target string) {
	ctx, cancel := context.WithTimeout(ctx, prewarmTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
	if err != nil {
		return
	}
	resp, err := c.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

func isLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return true
	}
	h := u.Hostname()
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// Prewarm implements Prewarmer.
func (p *OpenAICompatProvider) Prewarm(ctx context.Context) {
	prewarmConn(ctx, p.client, p.cfg.BaseURL)
}

// Prewarm implements Prewarmer.
func (p *ClaudeProvider) Prewarm(ctx context.Context) { prewarmConn(ctx, p.client, p.cfg.BaseURL) }

// Prewarm implements Prewarmer.
func (p *GeminiProvider) Prewarm(ctx context.Context) { prewarmConn(ctx, p.client, p.cfg.BaseURL) }

// Prewarm implements Prewarmer.
func (p *ChatGPTProvider) Prewarm(ctx context.Context) { prewarmConn(ctx, p.client, p.cfg.BaseURL) }
