package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/ask"
)

// serve runs a one-shot-per-connection herdr stand-in and forwards each
// decoded request. reply is the raw JSON line sent back.
func serve(t *testing.T, reply string) (string, <-chan map[string]any) {
	t.Helper()
	// short path: macOS caps unix socket paths near 104 bytes
	dir, err := os.MkdirTemp("", "hd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got := make(chan map[string]any, 8)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(c).ReadBytes('\n')
			var req map[string]any
			_ = json.Unmarshal(line, &req)
			got <- req
			_, _ = c.Write([]byte(reply + "\n"))
			c.Close()
		}
	}()
	return path, got
}

func TestUnixSenderRoundTrip(t *testing.T) {
	path, got := serve(t, `{"id":"x","result":{"type":"ok"}}`)
	err := unixSender{path: path}.send("pane.report_agent", map[string]any{"pane_id": "w1:p1", "state": "idle"})
	if err != nil {
		t.Fatal(err)
	}
	req := <-got
	if req["method"] != "pane.report_agent" {
		t.Fatalf("method = %v", req["method"])
	}
	params := req["params"].(map[string]any)
	if params["pane_id"] != "w1:p1" || params["state"] != "idle" {
		t.Fatalf("params = %v", params)
	}
	if id, _ := req["id"].(string); id == "" {
		t.Fatal("request id missing")
	}
}

func TestUnixSenderSurfacesServerErrorWithoutRetry(t *testing.T) {
	path, got := serve(t, `{"id":"x","error":{"code":"invalid_resume_argv","message":"nope"}}`)
	err := unixSender{path: path}.send("pane.report_agent", map[string]any{})
	if err == nil || err.Error() != "invalid_resume_argv: nope" {
		t.Fatalf("err = %v", err)
	}
	<-got
	select {
	case <-got:
		t.Fatal("server error was retried")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestUnixSenderDeadSocket(t *testing.T) {
	old := attempts
	attempts = []time.Duration{50 * time.Millisecond}
	defer func() { attempts = old }()
	if err := (unixSender{path: "/nonexistent/herdr.sock"}).send("m", nil); err == nil {
		t.Fatal("want dial error")
	}
}

// withDefault installs r as the process reporter for the test.
func withDefault(t *testing.T, r *Reporter) {
	t.Helper()
	defOnce.Do(func() {})
	old := def
	def = r
	t.Cleanup(func() { def = old })
}

type blockingApprover struct {
	seen chan struct{}
	gate chan struct{}
}

func (b blockingApprover) Request(context.Context, string, string, string) (approval.Decision, error) {
	close(b.seen)
	<-b.gate
	return approval.AllowOnce, nil
}

func TestWrapApproverBlocksWhileWaiting(t *testing.T) {
	r, f := newTest(10 * time.Millisecond)
	defer r.Close()
	withDefault(t, r)
	inner := blockingApprover{seen: make(chan struct{}), gate: make(chan struct{})}
	done := make(chan approval.Decision, 1)
	go func() {
		d, _ := WrapApprover(inner).Request(context.Background(), "rm -rf build\nsecond line", "k", "d")
		done <- d
	}()
	<-inner.seen
	waitStates(t, f, "blocked")
	if got := f.snapshot()[0].params["message"]; got != "approve: rm -rf build" {
		t.Fatalf("message = %v", got)
	}
	close(inner.gate)
	if d := <-done; d != approval.AllowOnce {
		t.Fatalf("decision = %v", d)
	}
	waitStates(t, f, "blocked", "idle")
}

// holdAsker answers only once released, so the block is observable.
type holdAsker struct{ gate chan struct{} }

func (h holdAsker) Ask(context.Context, ask.Question) (ask.Answer, error) {
	<-h.gate
	return ask.Answer{Index: 0, Text: "a"}, nil
}

func TestWrapAskerMessageUsesHeaderThenPrompt(t *testing.T) {
	r, f := newTest(10 * time.Millisecond)
	defer r.Close()
	withDefault(t, r)
	for i, q := range []ask.Question{
		{Header: "3D engine", Prompt: "which one?"},
		{Prompt: "which one?"},
	} {
		inner := holdAsker{gate: make(chan struct{})}
		done := make(chan struct{})
		go func() {
			_, _ = WrapAsker(inner).Ask(context.Background(), q)
			close(done)
		}()
		want := []string{"blocked", "idle", "blocked"}[:2*i+1]
		waitStates(t, f, want...)
		close(inner.gate)
		<-done
		waitStates(t, f, append(want, "idle")...)
	}
	calls := f.snapshot()
	if calls[0].params["message"] != "question: 3D engine" || calls[2].params["message"] != "question: which one?" {
		t.Fatalf("messages = %v / %v", calls[0].params["message"], calls[2].params["message"])
	}
}

func TestWrappersPassThroughWhenOff(t *testing.T) {
	withDefault(t, nil)
	var a approval.Approver = approval.Static{Verdict: approval.Deny}
	if WrapApprover(a) != a {
		t.Fatal("approver wrapped while reporting is off")
	}
	var k ask.Asker = ask.Static{}
	if WrapAsker(k) != k {
		t.Fatal("asker wrapped while reporting is off")
	}
}

func TestOneLineTruncates(t *testing.T) {
	long := ""
	for i := 0; i < 200; i++ {
		long += "é"
	}
	got := oneLine(long)
	if r := []rune(got); len(r) != blockMsgMax || r[len(r)-1] != '…' {
		t.Fatalf("len=%d got=%q", len([]rune(got)), got)
	}
}
