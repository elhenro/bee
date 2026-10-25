package tools

import (
	"context"
	"strings"
	"sync"
	"time"
)

// LiveTailLines is how many trailing output rows a running tool reports.
const LiveTailLines = 4

const (
	// bytes of recent output kept to find the last rows in
	liveTailBytes = 4 << 10
	// minimum gap between reports, so a chatty command can't flood the UI
	liveTailEvery = 150 * time.Millisecond
)

type liveSinkKey struct{}

// WithLiveSink returns a ctx whose tools may report the tail of their output
// while they run. sink must not block.
func WithLiveSink(ctx context.Context, sink func(tail string)) context.Context {
	return context.WithValue(ctx, liveSinkKey{}, sink)
}

// LiveSink returns the sink carried by ctx, nil when nobody is listening.
func LiveSink(ctx context.Context) func(tail string) {
	sink, _ := ctx.Value(liveSinkKey{}).(func(string))
	return sink
}

// TailWriter is an io.Writer that reports the last LiveTailLines non-blank
// rows written to it, throttled, with a trailing report so output that goes
// quiet still lands.
type TailWriter struct {
	sink func(string)

	mu       sync.Mutex
	buf      []byte
	lastSent string
	lastAt   time.Time
	timer    *time.Timer
	closed   bool
}

func NewTailWriter(sink func(string)) *TailWriter { return &TailWriter{sink: sink} }

// Write never fails.
func (w *TailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	if over := len(w.buf) - liveTailBytes; over > 0 {
		w.buf = w.buf[over:]
	}
	if w.closed || w.timer != nil {
		return len(p), nil
	}
	if wait := liveTailEvery - time.Since(w.lastAt); wait > 0 {
		w.timer = time.AfterFunc(wait, w.flush)
		return len(p), nil
	}
	w.emitLocked()
	return len(p), nil
}

// Close stops reports; call when the command is done.
func (w *TailWriter) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
}

func (w *TailWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.timer = nil
	if !w.closed {
		w.emitLocked()
	}
}

func (w *TailWriter) emitLocked() {
	w.lastAt = time.Now()
	tail := lastRows(string(w.buf[advanceToRuneStart(string(w.buf), 0):]), LiveTailLines)
	if tail == "" || tail == w.lastSent {
		return
	}
	w.lastSent = tail
	w.sink(tail)
}

// lastRows returns the last n non-blank rows of s. A carriage return rewrites
// the row, so only the text after the last one counts (progress bars).
func lastRows(s string, n int) string {
	rows := strings.Split(s, "\n")
	var out []string
	for i := len(rows) - 1; i >= 0 && len(out) < n; i-- {
		row := strings.TrimRight(rows[i], "\r")
		if j := strings.LastIndexByte(row, '\r'); j >= 0 {
			row = row[j+1:]
		}
		if strings.TrimSpace(row) == "" {
			continue
		}
		out = append(out, row)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return strings.Join(out, "\n")
}
