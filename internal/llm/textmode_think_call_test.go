package llm

import (
	"context"
	"testing"
)

// think tags inside an open tool call are file content, not reasoning, even
// when the tag is split across deltas.
func TestTextMode_ThinkTagInsideCallBodyIsContent(t *testing.T) {
	inner := &fakeProvider{events: []Event{
		{Type: EventTextDelta, Delta: `<think>plan</think><write>{"path":"a.html","content":"<thi`},
		{Type: EventTextDelta, Delta: `nk>x</think> hello"}</write>`},
		{Type: EventDone},
	}}
	ch, err := NewTextMode(inner, TextModeOptions{}).Stream(context.Background(), Request{Tools: coerceTools()})
	if err != nil {
		t.Fatal(err)
	}
	tools, _, think := collect(ch)
	if len(tools) != 1 || tools[0].Input["content"] != "<think>x</think> hello" {
		t.Fatalf("content corrupted: %+v", tools)
	}
	if think != "plan" {
		t.Fatalf("reasoning before the call must still be split off: %q", think)
	}
}
