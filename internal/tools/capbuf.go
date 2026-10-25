package tools

import (
	"fmt"
	"sync"
)

// CapBuffer is an io.Writer that keeps the first headMax and last tailMax
// bytes written and drops the middle, so a runaway command (`yes`) cannot grow
// memory without bound while the start and the end of its output survive.
type CapBuffer struct {
	mu      sync.Mutex
	headMax int
	tailMax int
	head    []byte
	tail    []byte
	dropped int
}

// NewCapBuffer returns a buffer capped at headMax+tailMax retained bytes.
func NewCapBuffer(headMax, tailMax int) *CapBuffer {
	return &CapBuffer{headMax: headMax, tailMax: tailMax}
}

// Write never fails; bytes past the caps are counted and dropped.
func (b *CapBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if room := b.headMax - len(b.head); room > 0 {
		k := min(room, len(p))
		b.head = append(b.head, p[:k]...)
		p = p[k:]
	}
	if len(p) == 0 {
		return n, nil
	}
	if b.tailMax <= 0 {
		b.dropped += len(p)
		return n, nil
	}
	b.tail = append(b.tail, p...)
	// trim lazily at 2x so a stream of small writes does not copy every time
	if len(b.tail) > 2*b.tailMax {
		over := len(b.tail) - b.tailMax
		b.dropped += over
		b.tail = append(b.tail[:0], b.tail[over:]...)
	}
	return n, nil
}

// String returns head, an omission marker when anything was dropped, and tail.
func (b *CapBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	head, tail := b.head, b.tail
	dropped := b.dropped
	if over := len(tail) - b.tailMax; over > 0 {
		dropped += over
		tail = tail[over:]
	}
	if dropped == 0 {
		return string(head) + string(tail)
	}
	h := trimPartialRune(string(head))
	t := string(tail[advanceToRuneStart(string(tail), 0):])
	skipped := dropped + (len(head) - len(h)) + (len(tail) - len(t))
	return fmt.Sprintf("%s\n...[%d bytes omitted]...\n%s", h, skipped, t)
}
