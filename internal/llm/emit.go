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
