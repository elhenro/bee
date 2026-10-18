package llm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// retryDelay is the backoff between pre-stream attempts. Var so tests can
// shrink it.
var retryDelay = retryBackoff

// postWithRetry sends the request build() makes, retrying network errors and
// retryable statuses (408/429/5xx/529) with the same backoff and caps as the
// chat-completions path. Only the pre-stream phase retries; a returned response
// is either 2xx or a status the caller should handle itself.
func postWithRetry(ctx context.Context, client *http.Client, name string, build func() (*http.Request, error)) (*http.Response, error) {
	var lastErr error
	lastWasRate := false
	for attempt := 0; attempt < maxRetryAttemptsRate; attempt++ {
		if attempt >= maxRetryAttempts && !lastWasRate {
			break
		}
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryDelay(attempt)):
			}
		}
		req, err := build()
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = fmt.Errorf("post: %w", err)
			lastWasRate = false
			continue
		}
		if !retryableStatus(resp.StatusCode) {
			return resp, nil
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		lastErr = fmt.Errorf("provider %s status %d: %s", name, resp.StatusCode, string(raw))
		lastWasRate = rateLimitedStatus(resp.StatusCode)
	}
	return nil, fmt.Errorf("provider %s exhausted retries: %w", name, lastErr)
}
