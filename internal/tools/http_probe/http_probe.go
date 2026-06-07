// Package http_probe implements the bee-wars raw-HTTP attacker tool. Unlike
// web_fetch it has NO SSRF/private-IP guard and does no markdown extraction —
// it returns the raw status, headers, and body so an agent can probe and
// exploit the opponent's private service. Containment is the container network
// namespace (an --internal Docker bridge), not this tool. Only registered in
// wars mode.
package http_probe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

const (
	toolName       = "http_probe"
	defaultMaxBody = 16384
	defaultTimeout = 15 * time.Second
)

// Tool issues raw HTTP requests to an arbitrary URL (typically the opponent).
type Tool struct {
	client *http.Client
	maxLen int
}

// New returns an http_probe tool with a bounded-timeout client.
func New() tools.Tool {
	return &Tool{
		client: &http.Client{Timeout: defaultTimeout},
		maxLen: defaultMaxBody,
	}
}

func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: toolName,
		Description: "Send a raw HTTP request to a URL (use it to probe and exploit the opponent's " +
			"service). Returns the status line, response headers, and body verbatim. No domain or " +
			"private-IP restrictions.",
		PromptSnippet: "raw HTTP to a target → status, headers, body",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"minLength":   1,
					"description": "Full target URL, e.g. http://blue:8080/ping?host=...",
				},
				"method": map[string]any{
					"type":        "string",
					"description": "HTTP method. Default GET.",
				},
				"body": map[string]any{
					"type":        "string",
					"description": "Optional request body (for POST/PUT).",
				},
				"headers": map[string]any{
					"type":        "object",
					"description": "Optional request headers as a string->string map.",
				},
			},
			"required": []string{"url"},
		},
	}
}

func (t *Tool) Run(ctx context.Context, input map[string]any) (tools.Result, error) {
	rawURL, _ := input["url"].(string)
	if strings.TrimSpace(rawURL) == "" {
		return tools.Result{Content: "missing or empty 'url'", IsError: true}, nil
	}
	method := "GET"
	if m, ok := input["method"].(string); ok && strings.TrimSpace(m) != "" {
		method = strings.ToUpper(strings.TrimSpace(m))
	}
	var body io.Reader
	if b, ok := input["body"].(string); ok && b != "" {
		body = strings.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return tools.Result{Content: fmt.Sprintf("build request: %v", err), IsError: true}, nil
	}
	if hdrs, ok := input["headers"].(map[string]any); ok {
		for k, v := range hdrs {
			if s, ok := v.(string); ok {
				req.Header.Set(k, s)
			}
		}
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return tools.Result{Content: fmt.Sprintf("request failed: %v", err), IsError: true}, nil
	}
	defer resp.Body.Close()

	payload, _ := io.ReadAll(io.LimitReader(resp.Body, int64(t.maxLen)+1))
	truncated := len(payload) > t.maxLen
	if truncated {
		payload = payload[:t.maxLen]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "HTTP %s\n", resp.Status)
	names := make([]string, 0, len(resp.Header))
	for k := range resp.Header {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		fmt.Fprintf(&b, "%s: %s\n", k, strings.Join(resp.Header[k], ", "))
	}
	b.WriteString("\n")
	b.Write(payload)
	if truncated {
		fmt.Fprintf(&b, "\n... (body truncated at %d bytes)", t.maxLen)
	}
	return tools.Result{Content: b.String()}, nil
}
