package remote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// gateEngine blocks in Send until released or its context ends, and reports
// which of the two ended the turn.
type gateEngine struct {
	started chan struct{}
	release chan struct{}
	ended   chan error
}

func newGateEngine() *gateEngine {
	return &gateEngine{started: make(chan struct{}), release: make(chan struct{}), ended: make(chan error, 1)}
}

func (g *gateEngine) Send(ctx context.Context, _ string, _ func(string)) (string, error) {
	close(g.started)
	select {
	case <-ctx.Done():
		g.ended <- ctx.Err()
		return "", ctx.Err()
	case <-g.release:
		g.ended <- nil
		return "finished", nil
	}
}

func sendRequest(s *Server, ctx context.Context) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/"+s.token+"/send", strings.NewReader(`{"text":"hi"}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// a client that drops mid-turn cancels its request context; the agent must
// finish its work anyway.
func TestSendSurvivesClientDisconnect(t *testing.T) {
	eng := newGateEngine()
	s := New(eng, Options{})
	reqCtx, dropClient := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Handler().ServeHTTP(httptest.NewRecorder(), sendRequest(s, reqCtx))
	}()
	<-eng.started
	dropClient()
	close(eng.release)
	<-done
	if err := <-eng.ended; err != nil {
		t.Fatalf("turn cancelled by client disconnect: %v", err)
	}
	if got := s.history[len(s.history)-1].Text; got != "finished" {
		t.Fatalf("assistant reply = %q, want %q", got, "finished")
	}
}

// shutting the server down ends an in-flight turn.
func TestServeShutdownCancelsTurn(t *testing.T) {
	eng := newGateEngine()
	s := New(eng, Options{Addr: "127.0.0.1:0"})
	ln, url, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- s.Serve(ctx, ln) }()

	go func() {
		resp, err := http.Post(url+"send", "application/json", strings.NewReader(`{"text":"hi"}`))
		if err == nil {
			resp.Body.Close()
		}
	}()
	<-eng.started
	stop()
	select {
	case err := <-eng.ended:
		if err == nil {
			t.Fatal("turn finished normally, want cancelled by shutdown")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("turn still running after shutdown")
	}
	<-served
}
