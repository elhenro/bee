package llm

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// sseMaxLine caps one SSE line. A tool call carrying a whole file can ride in a
// single chunk, so this sits well above typical lines.
const sseMaxLine = 4 * 1024 * 1024

// sseScanError wraps a scanner failure. An oversized line fails the same way on
// every replay, so it must not carry the "sse scan" marker the loop retries on.
func sseScanError(err error) error {
	if errors.Is(err, bufio.ErrTooLong) {
		return fmt.Errorf("sse line over %d bytes: %w", sseMaxLine, err)
	}
	return fmt.Errorf("sse scan: %w", err)
}

// handler results that end readSSE without an error event.
var (
	// errSSEDone is the stream-end marker: stop reading, finish normally.
	errSSEDone = errors.New("sse done")
	// errSSEStop means the consumer is gone: stop, nothing more to send.
	errSSEStop = errors.New("sse stop")
)

// send emits ev for an SSE handler, errSSEStop once the consumer is gone.
func send(ctx context.Context, out chan<- Event, ev Event) error {
	if !emit(ctx, out, ev) {
		return errSSEStop
	}
	return nil
}

// emitAll sends the events in order, giving up once the consumer is gone.
func emitAll(ctx context.Context, out chan<- Event, evs ...Event) {
	for _, ev := range evs {
		if !emit(ctx, out, ev) {
			return
		}
	}
}

// readSSE feeds every non-empty `data:` payload of resp to handle and owns what
// all streaming providers share: the inactivity watchdog (stall <= 0 disables
// it), cancellation, and the terminal error events. handle may call bump to
// count live output it produced as liveness. A handler error other than the
// sentinels above is emitted as an error event. Returns false when the stream
// ended in an error event, so the caller must not finish with a done event.
func readSSE(ctx context.Context, resp *http.Response, name string, stall time.Duration, out chan<- Event, handle func(payload []byte, bump func()) error) bool {
	bump, stalled, cancel := streamWatchdogWith(ctx, resp.Body, stall)
	defer cancel()
	stalledErr := func() error {
		return fmt.Errorf("provider %s stream stalled: no data for %s (try a different model)", name, stall)
	}
	fail := func(err error) bool {
		emit(ctx, out, Event{Type: EventError, Err: err})
		return false
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), sseMaxLine)
	for sc.Scan() {
		// any received line is liveness, including ': keepalive' comments and
		// blank separators some servers send during long reasoning phases;
		// without it the watchdog trips on keepalive-only traffic.
		bump()
		select {
		case <-ctx.Done():
			return fail(ctx.Err())
		case <-stalled:
			return fail(stalledErr())
		default:
		}
		payload, ok := bytes.CutPrefix(sc.Bytes(), []byte("data:"))
		if !ok {
			continue
		}
		if payload = bytes.TrimSpace(payload); len(payload) == 0 {
			continue
		}
		if err := handle(payload, bump); err != nil {
			if err == errSSEDone {
				break
			}
			if err == errSSEStop {
				return false
			}
			return fail(err)
		}
	}
	if err := sc.Err(); err != nil {
		// the watchdog closes the body, which surfaces here as a read error;
		// report the stall instead of the cryptic close
		select {
		case <-stalled:
			return fail(stalledErr())
		default:
		}
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		return fail(sseScanError(err))
	}
	return true
}
