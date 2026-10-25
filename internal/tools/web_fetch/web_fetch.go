package web_fetch

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"time"
)

// DomainPolicy controls which domains are allowed/blocked
type DomainPolicy struct {
	Allow []string
	Block []string
}

// IsAllowed checks if a domain is allowed based on the policy. Matching is
// case-insensitive and ignores a trailing root dot. The block list wins; an
// empty allow list allows everything not blocked.
func (p *DomainPolicy) IsAllowed(domain string) bool {
	domain = normHost(domain)
	matches := func(entry string) bool { return hostMatches(domain, entry) }
	if slices.ContainsFunc(p.Block, matches) {
		return false
	}
	return len(p.Allow) == 0 || slices.ContainsFunc(p.Allow, matches)
}

// DefaultDomainPolicy returns a default policy that allows all domains
func DefaultDomainPolicy() *DomainPolicy { return &DomainPolicy{} }

// FetchResult contains the result of a web fetch operation
type FetchResult struct {
	URL         string `json:"url"`
	Code        int    `json:"code"`
	CodeText    string `json:"code_text"`
	ContentType string `json:"content_type"`
	Bytes       int    `json:"bytes"`
	Markdown    string `json:"markdown"`
	DurationMs  int64  `json:"duration_ms"`
}

// MaxContentLen caps body read at 2x this value via io.LimitReader.
const MaxContentLen = 50000

// result renders the entry as a FetchResult for rawURL.
func (e *CacheEntry) result(rawURL string, start time.Time) *FetchResult {
	return &FetchResult{
		URL:         rawURL,
		Code:        e.Code,
		CodeText:    e.CodeText,
		ContentType: e.ContentType,
		Bytes:       e.Bytes,
		Markdown:    e.Content,
		DurationMs:  time.Since(start).Milliseconds(),
	}
}

// vetURL rejects URLs the fetcher must not request: non-web schemes, hosts
// the policy blocks and, when confined, hosts resolving to internal addresses.
func vetURL(rawURL string, policy *DomainPolicy, confined bool) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Only http/https. Reject file://, ftp://, gopher://, data:// etc — these
	// read local files or hit non-web services and are SSRF/local-disclosure
	// vectors. Enforced in every scope (the SSRF IP guard below is confined-only,
	// but file:// has no host to resolve so it must be blocked unconditionally).
	switch parsedURL.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("unsupported URL scheme %q: only http and https allowed", parsedURL.Scheme)
	}

	// Fast hostname blocklist pre-check (user allow/block lists, both scopes)
	host := parsedURL.Hostname()
	if policy != nil && !policy.IsAllowed(host) {
		return fmt.Errorf("domain %s is not allowed", host)
	}

	// SSRF guard (confined scope only): reject hosts that resolve to private/
	// loopback/unspecified/link-local IPs. danger-full-access skips it so the
	// model can reach localhost/LAN, just as it could via shell curl.
	if confined {
		addrs, err := net.LookupIP(host)
		if err != nil {
			return fmt.Errorf("DNS lookup failed for %s: %w", host, err)
		}
		for _, ip := range addrs {
			if isBlockedIP(ip) {
				return fmt.Errorf("host %s resolves to blocked IP %s", host, ip)
			}
		}
	}
	return nil
}

// newClient builds the HTTP client: timeout, redirect cap, per-hop policy
// re-check and the scope-dependent dialer.
func newClient(policy *DomainPolicy, confined bool) *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects (>%d)", 5)
			}
			// the initial allow/block policy check only covered the first host;
			// re-apply it to every redirect target.
			if policy != nil && !policy.IsAllowed(req.URL.Hostname()) {
				return fmt.Errorf("redirect to disallowed domain %s", req.URL.Hostname())
			}
			return nil
		},
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dialContext(confined),
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

// FetchURL fetches content from a URL and converts it to markdown. confined
// enables the SSRF guard (reject loopback/private/metadata hosts); under
// danger-full-access it is false and any reachable host is allowed.
func FetchURL(rawURL string, policy *DomainPolicy, confined bool) (*FetchResult, error) {
	start := time.Now()
	if err := vetURL(rawURL, policy, confined); err != nil {
		return nil, err
	}
	if entry, exists := cache.Get(rawURL); exists {
		return entry.result(rawURL, start), nil
	}

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "bee/0.1 (+https://github.com/elhenro/bee)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := newClient(policy, confined).Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	// Cap body read at MaxContentLen*2 to prevent OOM
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(MaxContentLen)*2))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Convert HTML to markdown; other content types pass through raw
	markdown, err := renderBody(resp.Header.Get("Content-Type"), body)
	if err != nil {
		return nil, fmt.Errorf("failed to convert HTML to markdown: %w", err)
	}

	entry := &CacheEntry{
		Bytes:       len(body),
		Code:        resp.StatusCode,
		CodeText:    resp.Status,
		ContentType: resp.Header.Get("Content-Type"),
		Content:     markdown,
		ExpiresAt:   time.Now().Add(cache.ttl),
	}

	// only successes are cached: a transient 5xx/429 must not stick for 15 minutes
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		cache.Set(rawURL, entry)
	}
	return entry.result(rawURL, start), nil
}

// dialContext is the transport dialer. Under a confined scope it validates the
// connection IP on EVERY dial (initial request and each redirect hop) and pins
// the connection to the validated address. This closes two SSRF bypasses the
// one-shot pre-check in FetchURL can't: a redirect to a private/metadata host,
// and DNS rebinding (validate-then-reconnect re-resolution) — the connection
// only ever lands on an IP we just checked. danger-full-access uses the plain
// dialer so localhost/LAN is reachable.
func dialContext(confined bool) func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if !confined {
		return dialer.DialContext
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		h, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, h)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if isBlockedIP(ip.IP) {
				return nil, fmt.Errorf("refused: %s resolves to blocked IP %s", h, ip.IP)
			}
		}
		var lastErr error
		for _, ip := range ips {
			conn, derr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if derr == nil {
				return conn, nil
			}
			lastErr = derr
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("no usable address for %s", h)
		}
		return nil, lastErr
	}
}
