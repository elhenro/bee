package llm

import (
	"context"
	"strings"
	"testing"
)

// runThink streams deltas through textmode and returns the dispatched tool
// names, concatenated thinking, and concatenated answer text.
func runThink(t *testing.T, deltas ...string) (calls []string, thinking, text string) {
	t.Helper()
	var evs []Event
	for _, d := range deltas {
		evs = append(evs, Event{Type: EventTextDelta, Delta: d})
	}
	evs = append(evs, Event{Type: EventDone, StopReason: "stop"})
	p := NewTextMode(&fakeProvider{name: "x", events: evs}, TextModeOptions{})
	ch, err := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	if err != nil {
		t.Fatal(err)
	}
	var tk, tx strings.Builder
	for ev := range ch {
		switch ev.Type {
		case EventToolUse:
			calls = append(calls, ev.ToolUse.Name)
		case EventThinkingDelta:
			tk.WriteString(ev.Delta)
		case EventTextDelta:
			tx.WriteString(ev.Delta)
		}
	}
	return calls, tk.String(), tx.String()
}

const quoted = `<shell>{"command":"rm -rf x"}</shell>`

func TestTextMode_InlineThinkCallNotDispatched(t *testing.T) {
	calls, thinking, text := runThink(t, "<think>maybe "+quoted+" no</think>all done")
	if len(calls) != 0 {
		t.Fatalf("call quoted in thought dispatched: %v", calls)
	}
	if !strings.Contains(thinking, quoted) {
		t.Errorf("thinking = %q, want the quoted call", thinking)
	}
	if text != "all done" {
		t.Errorf("text = %q, want %q", text, "all done")
	}
}

func TestTextMode_CallAfterThinkStillDispatched(t *testing.T) {
	calls, _, text := runThink(t, "<think>plan</think>\n", `<shell>{"command":"ls"}</shell>`)
	if len(calls) != 1 || calls[0] != "shell" {
		t.Fatalf("calls = %v, want [shell]", calls)
	}
	if text != "" {
		t.Errorf("text = %q, want empty", text)
	}
}

func TestTextMode_ThinkTagSplitAcrossDeltas(t *testing.T) {
	calls, thinking, text := runThink(t, "<thi", "nk>try "+quoted[:8], quoted[8:]+"</th", "INK>ok")
	if len(calls) != 0 {
		t.Fatalf("calls = %v, want none", calls)
	}
	if thinking != "try "+quoted {
		t.Errorf("thinking = %q", thinking)
	}
	if text != "ok" {
		t.Errorf("text = %q, want ok", text)
	}
}

func TestTextMode_UnclosedThinkSwallowsCall(t *testing.T) {
	calls, thinking, text := runThink(t, "<think>still going "+quoted)
	if len(calls) != 0 || text != "" {
		t.Fatalf("calls = %v, text = %q, want neither", calls, text)
	}
	if !strings.Contains(thinking, quoted) {
		t.Errorf("thinking = %q", thinking)
	}
}

func TestTextMode_ThinkingTagVariant(t *testing.T) {
	calls, _, text := runThink(t, "<Thinking>"+quoted+"</Thinking>fine")
	if len(calls) != 0 || text != "fine" {
		t.Fatalf("calls = %v, text = %q", calls, text)
	}
}

func TestTextMode_LoneAngleBracketKept(t *testing.T) {
	_, _, text := runThink(t, "a < b and c <", " d <th")
	if text != "a < b and c < d <th" {
		t.Errorf("text = %q", text)
	}
}

func TestThinkSplitter_Flush(t *testing.T) {
	var s thinkSplitter
	if text, think := s.feed("hi <thi"); text != "hi " || think != "" {
		t.Fatalf("feed = %q/%q", text, think)
	}
	if text, think := s.flush(); text != "<thi" || think != "" {
		t.Fatalf("flush = %q/%q", text, think)
	}
}
