package llm

import (
	"context"
	"net/http"
)

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

// respLoop drains one response onto out, owning both resp.Body and out.
type respLoop func(ctx context.Context, resp *http.Response, out chan<- Event)

// startStream runs the streaming or the single-body loop on a fresh event
// channel. Compaction and one-shot callers leave Request.Stream unset.
func startStream(ctx context.Context, resp *http.Response, stream bool, streaming, oneShot respLoop) <-chan Event {
	out := make(chan Event, 16)
	run := oneShot
	if stream {
		run = streaming
	}
	go run(ctx, resp, out)
	return out
}
