package llm

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestIsLoopbackURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"http://localhost:11434/v1":    true,
		"http://127.0.0.1:8000/v1":     true,
		"http://[::1]:8000/v1":         true,
		"https://openrouter.ai/api/v1": false,
		"https://api.anthropic.com/v1": false,
		"":                             true,
		"https://generativelanguage.googleapis.com/v1beta": false,
	} {
		if got := isLoopbackURL(raw); got != want {
			t.Errorf("isLoopbackURL(%q) = %v, want %v", raw, got, want)
		}
	}
}

// the warm-up leaves a pooled connection the first real request reuses.
func TestWarmLeavesReusableConnection(t *testing.T) {
	var conns atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	defer srv.Close()

	c := &http.Client{Transport: &http.Transport{}}
	warm(context.Background(), c, srv.URL)
	resp, err := c.Post(srv.URL+"/chat/completions", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if n := conns.Load(); n != 1 {
		t.Fatalf("server saw %d connections, want 1 (warm-up reused)", n)
	}
}
