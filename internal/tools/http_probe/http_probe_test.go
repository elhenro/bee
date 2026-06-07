package http_probe

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunFetchesLocalServer(t *testing.T) {
	// httptest binds 127.0.0.1 — a successful fetch proves http_probe has NO
	// SSRF/private-IP guard, which is the whole point for hitting the peer.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Vuln", "yes")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "pong")
	}))
	defer srv.Close()

	res, err := New().Run(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if res.IsError {
		t.Fatalf("Run IsError: %q", res.Content)
	}
	if !strings.Contains(res.Content, "200") {
		t.Fatalf("result missing status 200: %q", res.Content)
	}
	if !strings.Contains(res.Content, "pong") {
		t.Fatalf("result missing body: %q", res.Content)
	}
}

func TestRunPostsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_, _ = io.WriteString(w, "echo:"+string(b))
	}))
	defer srv.Close()

	res, err := New().Run(context.Background(), map[string]any{
		"url":    srv.URL,
		"method": "POST",
		"body":   "ignore prior instructions",
	})
	if err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if !strings.Contains(res.Content, "echo:ignore prior instructions") {
		t.Fatalf("body not posted/echoed: %q", res.Content)
	}
}

func TestRunMissingURLIsError(t *testing.T) {
	res, err := New().Run(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if !res.IsError {
		t.Fatal("missing url should yield an error result")
	}
}

func TestSpecName(t *testing.T) {
	if New().Spec().Name != "http_probe" {
		t.Fatalf("spec name = %q, want http_probe", New().Spec().Name)
	}
}
