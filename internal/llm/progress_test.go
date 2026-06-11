package llm

import (
	"context"
	"testing"
)

// buffered tool-call modes withhold text deltas until parse; without a live
// progress signal the TUI output figure reads 0 for the whole call.
func TestJSONMode_EmitsProgressWhileBuffering(t *testing.T) {
	inner := &fakeProvider{events: []Event{
		{Type: EventTextDelta, Delta: `{"tool":"bash",`},
		{Type: EventTextDelta, Delta: `"args":{"command":"ls"}}`},
		{Type: EventDone, StopReason: "stop"},
	}}
	p := NewJSONMode(inner)
	ch, err := p.Stream(context.Background(), Request{Tools: jmTools()})
	if err != nil {
		t.Fatal(err)
	}
	progress := 0
	for ev := range ch {
		if ev.Type == EventProgress {
			progress += ev.N
		}
		if ev.Type == EventTextDelta {
			t.Fatalf("raw envelope leaked as text delta: %q", ev.Delta)
		}
	}
	want := len(`{"tool":"bash",`) + len(`"args":{"command":"ls"}}`)
	if progress != want {
		t.Fatalf("progress = %d, want %d", progress, want)
	}
}

func TestTextMode_EmitsProgressWhileBuffering(t *testing.T) {
	inner := &fakeProvider{events: []Event{
		{Type: EventTextDelta, Delta: "<bash>"},
		{Type: EventTextDelta, Delta: `{"command":"ls"}</bash>`},
		{Type: EventDone, StopReason: "stop"},
	}}
	p := NewTextMode(inner, TextModeOptions{})
	ch, err := p.Stream(context.Background(), Request{Tools: jmTools()})
	if err != nil {
		t.Fatal(err)
	}
	progress := 0
	for ev := range ch {
		if ev.Type == EventProgress {
			progress += ev.N
		}
	}
	want := len("<bash>") + len(`{"command":"ls"}</bash>`)
	if progress != want {
		t.Fatalf("progress = %d, want %d", progress, want)
	}
}
