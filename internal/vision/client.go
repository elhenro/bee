// Package vision describes images via a secondary multimodal model. It exists
// so a non-vision main model can still work with screenshots and pasted images:
// the loop routes image blocks here, gets text back, and injects that text.
//
// Two wire shapes are supported: an OpenAI-compatible /chat/completions endpoint
// (works with omlx / LM Studio / vLLM / hosted qwen-VL) and ollama /api/generate.
package vision

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

const requestTimeout = 90 * time.Second

const defaultQuestion = "Describe this image in detail for a coding agent: " +
	"any visible text verbatim, layout, UI elements, errors, and code."

// Client points at a multimodal model. API selects the wire shape:
// "openai" (default) or "ollama". APIKey is optional (local servers skip auth).
type Client struct {
	Model    string
	Endpoint string
	APIKey   string
	API      string
}

// Describe sends one image and returns the model's text description. mediaType
// is e.g. "image/png"; it defaults to image/png when empty. question overrides
// the default prompt.
func (c Client) Describe(ctx context.Context, img []byte, mediaType, question string) (string, error) {
	if c.Model == "" || c.Endpoint == "" {
		return "", fmt.Errorf("vision: model and endpoint required")
	}
	if mediaType == "" {
		mediaType = "image/png"
	}
	if question == "" {
		question = defaultQuestion
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	if strings.EqualFold(c.API, "ollama") {
		return c.describeOllama(cctx, img, question)
	}
	return c.describeOpenAI(cctx, img, mediaType, question)
}

func (c Client) describeOpenAI(ctx context.Context, img []byte, mediaType, question string) (string, error) {
	dataURL := "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(img)
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	err := c.post(ctx, "/chat/completions", map[string]any{
		"model":  c.Model,
		"stream": false,
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": question},
				{"type": "image_url", "image_url": map[string]string{"url": dataURL}},
			},
		}},
	}, &out)
	if err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("vision: empty response")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

func (c Client) describeOllama(ctx context.Context, img []byte, question string) (string, error) {
	var out struct {
		Response string `json:"response"`
	}
	err := c.post(ctx, "/api/generate", map[string]any{
		"model":  c.Model,
		"prompt": question,
		"images": []string{base64.StdEncoding.EncodeToString(img)},
		"stream": false,
	}, &out)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Response), nil
}

// post sends body as JSON to the endpoint's path and decodes the 200 reply
// into out.
func (c Client) post(ctx context.Context, path string, body map[string]any, out any) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.Endpoint, "/")+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("vision request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("vision endpoint returned %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("vision decode failed: %w", err)
	}
	return nil
}
