package llm

import (
	"context"
	"io"
	"net"
	"net/http"
	"time"
)

// streamStallTimeout aborts a streaming request when no SSE line has arrived
// for this long. Tuned for slow reasoning models (kimi-k2.6, deepseek-v4,
// glm-4.6 thinking): first-byte can legitimately take ~60s on long prompts,
// but mid-stream gaps longer than 5min mean the provider is idle-holding.
//
// Total wall-clock is intentionally unbounded — a 10-minute reasoning trace
// is legitimate. The watchdog only cares about *silence*.
// Per-call override available via OpenAICompatConfig.StallTimeout.
const streamStallTimeout = 5 * time.Minute

// newStreamingClient builds an http.Client suitable for SSE.
//
// Critically, the overall Client.Timeout is left at zero: it applies to the
// entire request lifecycle including body read, so any non-zero value would
// kill a long-running stream mid-flight and surface as
// "context deadline exceeded (Client.Timeout or context cancellation while
// reading body)". For streams we rely on:
//   - transport-level timeouts (dial, TLS, response headers)
//   - the per-call ctx for cancellation
//   - streamWatchdog for idle-hold detection
func newStreamingClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 90 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			ForceAttemptHTTP2:     true,
		},
		// Timeout intentionally unset — see comment above.
	}
}

// streamWatchdog runs an inactivity timer alongside an SSE scan loop.
//
// Returns:
//   - bump:    call after every successful scanner.Scan() to reset the timer
//   - stalled: closed when no bump has arrived for streamStallTimeout
//   - cancel:  call from a defer to stop the watchdog goroutine
//
// On stall the watchdog closes body to unblock the scanner. The caller's
// select on `stalled` then surfaces a clean error instead of the cryptic
// "context deadline exceeded while reading body" from http.Client.Timeout.
func streamWatchdog(ctx context.Context, body io.Closer) (bump func(), stalled <-chan struct{}, cancel func()) {
	return streamWatchdogWith(ctx, body, streamStallTimeout)
}

// streamWatchdogWith is streamWatchdog with a caller-supplied timeout. A
// non-positive timeout returns a no-op watchdog (bump/cancel are noops, the
// stalled channel never fires) — useful in tests and for callers that opt
// out per-config.
func streamWatchdogWith(ctx context.Context, body io.Closer, timeout time.Duration) (bump func(), stalled <-chan struct{}, cancel func()) {
	if timeout <= 0 {
		stalledCh := make(chan struct{})
		return func() {}, stalledCh, func() {}
	}
	wdCtx, wdCancel := context.WithCancel(ctx)
	activity := make(chan struct{}, 1)
	stalledCh := make(chan struct{})

	go func() {
		t := time.NewTimer(timeout)
		defer t.Stop()
		for {
			select {
			case <-wdCtx.Done():
				return
			case <-activity:
				if !t.Stop() {
					select {
					case <-t.C:
					default:
					}
				}
				t.Reset(timeout)
			case <-t.C:
				close(stalledCh)
				_ = body.Close() // unblock scanner.Scan()
				return
			}
		}
	}()

	bump = func() {
		select {
		case activity <- struct{}{}:
		default:
		}
	}
	return bump, stalledCh, wdCancel
}
