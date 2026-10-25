package llm

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"
)

// streamStallTimeout aborts a streaming request when no SSE line has arrived
// for this long. Tuned for slow reasoning models: first-byte can legitimately
// take ~60s on long prompts. The default is generous because some servers
// buffer the whole reasoning trace server-side and emit nothing until it is
// done — a 10-15min thinking phase then arrives as one burst, which a tight
// window would false-positive as a stall.
//
// Total wall-clock is intentionally unbounded — a long reasoning trace is
// legitimate. The watchdog only cares about *silence*. Override globally with
// BEE_STREAM_STALL_SECONDS, or per-call via OpenAICompatConfig.StallTimeout.
const streamStallTimeout = 10 * time.Minute

// effectiveStallTimeout returns the default stall window, overridden by
// BEE_STREAM_STALL_SECONDS when set to a positive integer. Anything <=0 there
// disables the watchdog entirely (the no-op path in streamWatchdog).
func effectiveStallTimeout() time.Duration {
	if v := os.Getenv("BEE_STREAM_STALL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return time.Duration(n) * time.Second
		}
	}
	return streamStallTimeout
}

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
//
// The header wait uses the stall window rather than a tight cap: servers that
// only flush headers with the first token (slow prefill on a long prompt) are
// alive, not stalled. A disabled watchdog leaves the wait to the ctx.
func newStreamingClient() *http.Client {
	headerWait := max(effectiveStallTimeout(), 0)
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: headerWait,
			ExpectContinueTimeout: 1 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			ForceAttemptHTTP2:     true,
		},
		// Timeout intentionally unset — see comment above.
	}
}

// setJSONHeaders sets the content negotiation of a JSON POST that answers with
// an SSE stream or a single JSON body.
func setJSONHeaders(r *http.Request, streaming bool) {
	r.Header.Set("Content-Type", "application/json")
	if streaming {
		r.Header.Set("Accept", "text/event-stream")
	} else {
		r.Header.Set("Accept", "application/json")
	}
}

// streamWatchdog runs an inactivity timer alongside an SSE scan loop.
//
// Returns:
//   - bump:    call after every successful scanner.Scan() to reset the timer
//   - stalled: closed when no bump has arrived for timeout
//   - cancel:  call from a defer to stop the watchdog goroutine
//
// On stall the watchdog closes body to unblock the scanner. The caller's
// select on `stalled` then surfaces a clean error instead of the cryptic
// "context deadline exceeded while reading body" from http.Client.Timeout. A
// non-positive timeout returns a no-op watchdog (bump/cancel are noops, the
// stalled channel never fires) — useful in tests and for callers that opt
// out per-config.
func streamWatchdog(ctx context.Context, body io.Closer, timeout time.Duration) (bump func(), stalled <-chan struct{}, cancel func()) {
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
