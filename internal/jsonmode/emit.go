// Package jsonmode emits NDJSON events for bee's --json output mode.
package jsonmode

import (
	"encoding/json"
	"io"
	"sync"
)

// Event is one NDJSON record.
type Event struct {
	Type    string `json:"type"`
	Delta   string `json:"delta,omitempty"`
	Name    string `json:"name,omitempty"`
	Input   any    `json:"input,omitempty"`
	UseID   string `json:"use_id,omitempty"`
	Content string `json:"content,omitempty"`
	Error   bool   `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
	Usage   *Usage `json:"usage,omitempty"`
}

// Usage mirrors llm.Usage but stays decoupled to avoid an import cycle.
type Usage struct {
	Input  int `json:"input"`
	Output int `json:"output"`
}

// Emitter writes NDJSON events to an io.Writer, safe for concurrent use.
type Emitter struct {
	mu  sync.Mutex
	enc *json.Encoder
}

// New returns an Emitter that writes one JSON line per Emit call to w.
func New(w io.Writer) *Emitter {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &Emitter{enc: enc}
}

// Emit serializes ev to a single JSON line. Errors are silently ignored —
// the run is effectively over if stdout is broken.
func (e *Emitter) Emit(ev Event) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	_ = e.enc.Encode(ev)
}
