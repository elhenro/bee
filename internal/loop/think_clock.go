package loop

import "time"

// thinkClock times one reasoning phase of a streamed turn: from the first
// reasoning delta to the first answer or tool-call event, or to the last
// reasoning delta when the stream ends inside the reasoning.
type thinkClock struct {
	start, end time.Time
	closed     bool
}

func (c *thinkClock) reasoning() {
	now := time.Now()
	if c.start.IsZero() {
		c.start = now
	}
	if !c.closed {
		c.end = now
	}
}

// answered closes the phase at the first non-reasoning content event
func (c *thinkClock) answered() {
	if c.start.IsZero() || c.closed {
		return
	}
	c.end, c.closed = time.Now(), true
}

// ms is the phase length; 1 at minimum when any reasoning arrived, so a
// block that streamed in one burst still counts as timed
func (c *thinkClock) ms() int64 {
	if c.start.IsZero() {
		return 0
	}
	return max(c.end.Sub(c.start).Milliseconds(), 1)
}
