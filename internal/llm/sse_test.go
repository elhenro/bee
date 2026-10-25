package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func sseResp(body string) *http.Response {
	return &http.Response{Body: io.NopCloser(strings.NewReader(body))}
}

func TestReadSSE_FeedsOnlyDataPayloads(t *testing.T) {
	body := ": keepalive\n\nevent: x\ndata: one\ndata:\ndata:   two  \n\ndata:[DONE]\ndata: never\n"
	var got []string
	out := make(chan Event, 4)
	ok := readSSE(context.Background(), sseResp(body), "p", 0, out, func(p []byte, _ func()) error {
		if string(p) == "[DONE]" {
			return errSSEDone
		}
		got = append(got, string(p))
		return nil
	})
	if !ok || len(out) != 0 {
		t.Fatalf("ok=%v events=%d, want a clean finish", ok, len(out))
	}
	if strings.Join(got, "|") != "one|two" {
		t.Errorf("payloads = %q", got)
	}
}

func TestReadSSE_HandlerErrorBecomesErrorEvent(t *testing.T) {
	boom := errors.New("boom")
	out := make(chan Event, 4)
	ok := readSSE(context.Background(), sseResp("data: x\ndata: y\n"), "p", 0, out, func([]byte, func()) error { return boom })
	if ok {
		t.Fatal("readSSE reported success after a handler error")
	}
	if ev := <-out; ev.Type != EventError || ev.Err != boom {
		t.Errorf("event = %+v", ev)
	}
}

func TestReadSSE_StopSentinelIsSilent(t *testing.T) {
	out := make(chan Event, 4)
	ok := readSSE(context.Background(), sseResp("data: x\n"), "p", 0, out, func([]byte, func()) error { return errSSEStop })
	if ok || len(out) != 0 {
		t.Fatalf("ok=%v events=%d, want a silent stop", ok, len(out))
	}
}

func TestReadSSE_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := make(chan Event, 4)
	if readSSE(ctx, sseResp("data: x\n"), "p", 0, out, func([]byte, func()) error { return nil }) {
		t.Fatal("readSSE reported success on a cancelled context")
	}
	if ev := <-out; ev.Err != context.Canceled {
		t.Errorf("event = %+v", ev)
	}
}
