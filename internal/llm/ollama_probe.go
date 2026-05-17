package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// OllamaBaseURL trims the trailing "/v1" segment from an openai-compat base
// URL so ollama's native paths (/api/show, /api/tags) resolve cleanly.
// Returns the input unchanged when no /v1 suffix is present.
func OllamaBaseURL(baseURL string) string {
	u := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(u, "/v1") {
		u = strings.TrimSuffix(u, "/v1")
	}
	return u
}

// ProbeOllamaContext POSTs to <baseURL>/api/show with {"name": modelID}, then
// extracts the model's real context length from either the `parameters`
// free-form string (`num_ctx <N>`) or the `model_info` map (any key ending in
// `.context_length`). Best-effort: returns (0, nil) when the endpoint exists
// but yields nothing usable, and (0, err) only on network/HTTP failures so
// callers can log without aborting.
func ProbeOllamaContext(ctx context.Context, client *http.Client, baseURL, modelID string) (int, error) {
	if client == nil {
		return 0, fmt.Errorf("nil http client")
	}
	if modelID == "" {
		return 0, fmt.Errorf("empty model id")
	}

	url := OllamaBaseURL(baseURL) + "/api/show"
	body, err := json.Marshal(map[string]string{"name": modelID})
	if err != nil {
		return 0, fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("post %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("status %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, fmt.Errorf("read: %w", err)
	}

	var payload struct {
		Parameters string         `json:"parameters"`
		ModelInfo  map[string]any `json:"model_info"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0, fmt.Errorf("decode: %w", err)
	}

	if n := numCtxFromParameters(payload.Parameters); n > 0 {
		return n, nil
	}
	if n := contextLengthFromModelInfo(payload.ModelInfo); n > 0 {
		return n, nil
	}
	return 0, nil
}

var numCtxLine = regexp.MustCompile(`(?m)^\s*num_ctx\s+(\d+)\s*$`)

// numCtxFromParameters scans the ollama `parameters` blob — a whitespace-
// separated key/value text section — for an explicit num_ctx override. This
// is the only value that reflects what the runtime will actually allocate.
func numCtxFromParameters(s string) int {
	if s == "" {
		return 0
	}
	m := numCtxLine.FindStringSubmatch(s)
	if len(m) < 2 {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

// contextLengthFromModelInfo finds the first `<arch>.context_length` entry in
// the model_info map. Ollama keys these by architecture (e.g. "llama.context_length",
// "qwen2.context_length"), so we don't hard-code the prefix.
func contextLengthFromModelInfo(info map[string]any) int {
	for k, v := range info {
		if !strings.HasSuffix(k, ".context_length") {
			continue
		}
		switch x := v.(type) {
		case float64:
			if x > 0 {
				return int(x)
			}
		case int:
			if x > 0 {
				return x
			}
		case json.Number:
			if n, err := x.Int64(); err == nil && n > 0 {
				return int(n)
			}
		}
	}
	return 0
}
