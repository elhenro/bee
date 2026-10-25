package llm

import (
	"bufio"
	"context"
	"errors"
	"fmt"
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

// relayInner forwards an inner stream to out for the wrappers that buffer text
// and tool calls themselves: text deltas go to onText, native tool calls to
// onTool, everything else passes through. Returns the inner done event (a
// plain stop when the stream ended without one), or ok=false once an error was
// forwarded.
func relayInner(ctx context.Context, in <-chan Event, out chan<- Event, onText, onTool func(Event)) (done Event, ok bool) {
	// an early return must not strand the inner producer on a full channel
	defer func() {
		go func() {
			for range in {
			}
		}()
	}()
	done = Event{Type: EventDone, StopReason: "stop"}
	for ev := range in {
		switch ev.Type {
		case EventTextDelta:
			onText(ev)
		case EventToolUse:
			onTool(ev)
		case EventDone:
			done = ev
		case EventError:
			emit(ctx, out, ev)
			return done, false
		default:
			emit(ctx, out, ev)
		}
	}
	return done, true
}

// emit sends ev on out, giving up when ctx is cancelled so a consumer that
// stopped reading cannot strand the producer goroutine. A free buffer slot is
// taken first, so the terminal error after a cancel still lands when it fits.
// Returns false when the event was dropped.
func emit(ctx context.Context, out chan<- Event, ev Event) bool {
	select {
	case out <- ev:
		return true
	default:
	}
	select {
	case out <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}
