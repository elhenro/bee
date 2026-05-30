package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const visionTimeout = 60 * time.Second

type visionClient struct {
	model    string
	endpoint string // ollama base url
}

// describe sends a PNG to the ollama generate endpoint and returns the text.
func (vc visionClient) describe(ctx context.Context, png []byte, question string) (string, error) {
	if question == "" {
		question = "Describe this web page screenshot: layout, visible text, and interactive elements."
	}
	body, _ := json.Marshal(map[string]any{
		"model":  vc.model,
		"prompt": question,
		"images": []string{base64.StdEncoding.EncodeToString(png)},
		"stream": false,
	})
	url := strings.TrimRight(vc.endpoint, "/") + "/api/generate"

	cctx, cancel := context.WithTimeout(ctx, visionTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("vision request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vision endpoint returned %d", resp.StatusCode)
	}
	var out struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("vision decode failed: %w", err)
	}
	return strings.TrimSpace(out.Response), nil
}
