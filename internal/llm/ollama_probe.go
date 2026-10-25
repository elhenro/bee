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
	return strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/v1")
}

// ollamaShowResp is the part of /api/show that carries the context length.
type ollamaShowResp struct {
	Parameters string         `json:"parameters"`
	ModelInfo  map[string]any `json:"model_info"`
}

// ollamaShow POSTs <root>/api/show with {"name": modelID}.
func ollamaShow(ctx context.Context, client *http.Client, root, modelID string) (ollamaShowResp, error) {
	var out ollamaShowResp
	url := root + "/api/show"
	body, _ := json.Marshal(map[string]string{"name": modelID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return out, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return out, fmt.Errorf("post %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return out, fmt.Errorf("decode: %w", err)
	}
	return out, nil
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
	show, err := ollamaShow(ctx, client, OllamaBaseURL(baseURL), modelID)
	if err != nil {
		return 0, err
	}
	if n := numCtxFromParameters(show.Parameters); n > 0 {
		return n, nil
	}
	return contextLengthFromModelInfo(show.ModelInfo), nil
}

var numCtxLine = regexp.MustCompile(`(?m)^\s*num_ctx\s+(\d+)\s*$`)

// numCtxFromParameters scans the ollama `parameters` blob — a whitespace-
// separated key/value text section — for an explicit num_ctx override. This
// is the only value that reflects what the runtime will actually allocate.
func numCtxFromParameters(s string) int {
	m := numCtxLine.FindStringSubmatch(s)
	if m == nil {
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
