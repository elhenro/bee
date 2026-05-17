package jsonmode

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestEmitter_NDJSON(t *testing.T) {
	var buf bytes.Buffer
	e := New(&buf)
	e.Emit(Event{Type: "text", Delta: "hello"})
	e.Emit(Event{Type: "tool_use", Name: "shell", UseID: "u1"})
	e.Emit(Event{Type: "done", Usage: &Usage{Input: 10, Output: 5}})

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d: %q", len(lines), buf.String())
	}

	var first Event
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first.Type != "text" || first.Delta != "hello" {
		t.Errorf("unexpected: %+v", first)
	}

	var third Event
	if err := json.Unmarshal([]byte(lines[2]), &third); err != nil {
		t.Fatal(err)
	}
	if third.Type != "done" || third.Usage == nil || third.Usage.Input != 10 {
		t.Errorf("unexpected: %+v", third)
	}
}

func TestEmitter_NilSafe(t *testing.T) {
	var e *Emitter
	e.Emit(Event{Type: "text"}) // must not panic
}
