package llm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

// retry policy for the pre-stream request: network errors and 408/429/5xx
// trigger an exp-backoff retry. Once the stream starts emitting deltas we
// no longer retry. replaying would duplicate tokens.
const (
	maxRetryAttempts     = 3
	maxRetryAttemptsRate = 5
	retryBaseDelay       = 800 * time.Millisecond
	retryMinDelay        = 200 * time.Millisecond
)

// retryRand drives the backoff jitter. seeded once per process. jitter does
// not need cryptographic randomness, only crowd-spreading.
var retryRand = rand.New(rand.NewSource(time.Now().UnixNano()))
var retryRandMu sync.Mutex

// retryableStatus reports whether a server response code is worth retrying.
// 408 request timeout, 429 too many requests, 5xx server errors, 529 overloaded. matches
// the set used by major chat-completions SDKs.
func retryableStatus(code int) bool {
	switch code {
	case 408, 429, 500, 502, 503, 504, 529:
		return true
	}
	return false
}

// rateLimitedStatus marks codes that warrant extra retries since the server
// is asking us to slow down rather than reporting a hard failure.
func rateLimitedStatus(code int) bool {
	return code == 429 || code == 503 || code == 529
}

// retryBackoff computes the delay for the given attempt with ±25% jitter,
// floored at retryMinDelay so jitter cannot push below the safe minimum.
func retryBackoff(attempt int) time.Duration {
	base := retryBaseDelay << (attempt - 1)
	retryRandMu.Lock()
	// jitter in [-0.25, +0.25]
	jitter := (retryRand.Float64()*0.5 - 0.25)
	retryRandMu.Unlock()
	return max(time.Duration(float64(base)*(1+jitter)), retryMinDelay)
}

// retryDelay is the backoff between pre-stream attempts. Var so tests can
// shrink it.
var retryDelay = retryBackoff

// postJSON is postWithRetry for a JSON body; apply sets the provider's headers
// on every attempt.
func postJSON(ctx context.Context, client *http.Client, name, url string, body []byte, apply func(*http.Request)) (*http.Response, error) {
	return postWithRetry(ctx, client, name, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		apply(req)
		return req, nil
	})
}

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
		lastErr = checkStatus(resp, name)
		lastWasRate = rateLimitedStatus(resp.StatusCode)
	}
	return nil, fmt.Errorf("provider %s exhausted retries: %w", name, lastErr)
}

// checkStatus turns a status >= 400 response into the provider error, closing
// its body. Any other response is left to the caller and yields nil.
func checkStatus(resp *http.Response, name string) error {
	if resp.StatusCode < 400 {
		return nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("provider %s status %d: %s", name, resp.StatusCode, raw)
}
