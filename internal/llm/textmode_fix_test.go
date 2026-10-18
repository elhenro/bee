package llm

import (
	"context"
	"strings"
	"testing"
)

func tmCalls(t *testing.T, deltas []string) (calls []Event, text string) {
	t.Helper()
	tm := NewTextMode(&fakeScript{deltas}, TextModeOptions{})
	ch, err := tm.Stream(context.Background(), Request{Tools: []ToolSpec{{Name: "write"}, {Name: "bash"}}})
	if err != nil {
		t.Fatal(err)
	}
	for ev := range ch {
		switch ev.Type {
		case EventToolUse:
			calls = append(calls, ev)
		case EventTextDelta:
			text += ev.Delta
		}
	}
	return calls, text
}

type fakeScript struct{ deltas []string }

func (f *fakeScript) Name() string { return "fake" }
func (f *fakeScript) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	ch := make(chan Event, len(f.deltas)+1)
	for _, d := range f.deltas {
		ch <- Event{Type: EventTextDelta, Delta: d}
	}
	ch <- Event{Type: EventDone, StopReason: "stop"}
	close(ch)
	return ch, nil
}

func TestTextModeKeepsAttributesInsideJSONBody(t *testing.T) {
	body := `<div class=\"box\" id=\"x\">hi</div><a href='y'>l</a>`
	calls, _ := tmCalls(t, []string{`<write>{"path":"a.html","content":"` + body + `"}</write>`})
	if len(calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(calls))
	}
	in := calls[0].ToolUse.Input
	want := `<div class="box" id="x">hi</div><a href='y'>l</a>`
	if in["content"] != want || in["path"] != "a.html" {
		t.Errorf("content mangled: %#v", in)
	}
}

func TestTextModeAttrFormStillWorksForKnownTool(t *testing.T) {
	calls, _ := tmCalls(t, []string{`<bash command="ls -la"/>`})
	if len(calls) != 1 || calls[0].ToolUse.Input["command"] != "ls -la" {
		t.Errorf("attr envelope regressed: %+v", calls)
	}
}

func TestTextModeUnknownTagWithAttrsIsLeftAsText(t *testing.T) {
	calls, text := tmCalls(t, []string{`see <div class="a">x</div> ok`})
	if len(calls) != 0 || !strings.Contains(text, `<div class="a">x</div>`) {
		t.Errorf("unknown tag must stay prose, calls=%d text=%q", len(calls), text)
	}
}

func TestTextModeNoEarlyDispatchOnInnerCloseTag(t *testing.T) {
	// first chunk ends inside write's JSON string, after an inner </bash>
	deltas := []string{
		`<write>{"path":"a.md","content":"run <bash>{\"command\":\"ls\"}</bash>`,
		` to list"}</write>`,
	}
	calls, _ := tmCalls(t, deltas)
	if len(calls) != 1 || calls[0].ToolUse.Name != "write" {
		t.Fatalf("want exactly the write call, got %+v", calls)
	}
	if c, _ := calls[0].ToolUse.Input["content"].(string); !strings.HasSuffix(c, " to list") {
		t.Errorf("write dispatched on partial body: %#v", calls[0].ToolUse.Input)
	}
}

func TestCompleteCallsEnd(t *testing.T) {
	known := map[string]bool{"write": true, "bash": true}
	s := `<bash>{"command":"ls"}</bash> text <write>{"c":"</bash>"`
	end, saw := completeCallsEnd(s, known)
	if !saw || s[:end] != `<bash>{"command":"ls"}</bash>` {
		t.Errorf("end=%d saw=%v head=%q", end, saw, s[:end])
	}
	if _, saw := completeCallsEnd("plain text", known); saw {
		t.Error("no tags, nothing seen")
	}
}
