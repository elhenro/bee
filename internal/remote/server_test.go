package remote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type stubEngine struct{}

func (stubEngine) Send(ctx context.Context, text string, onDelta func(string)) (string, error) {
	onDelta("hello ")
	onDelta("world")
	return "hello world", nil
}

func TestIndex(t *testing.T) {
	s := New(stubEngine{}, Options{Title: "test bee"})
	rec := httptest.NewRecorder()
	path := "/" + s.token + "/"
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(strings.ToLower(body), "<html") {
		t.Error("body missing <html")
	}
	if !strings.Contains(body, "<form") || !strings.Contains(body, "EventSource") {
		t.Error("body missing form/script")
	}
	if !strings.Contains(body, "test bee") {
		t.Error("title not injected")
	}
}

func TestUnknownPathReturns404(t *testing.T) {
	s := New(stubEngine{}, Options{})
	rec := httptest.NewRecorder()
	// root without token should 404, not expose anything
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for unknown path", rec.Code)
	}
}

func TestSend(t *testing.T) {
	s := New(stubEngine{}, Options{})
	rec := httptest.NewRecorder()
	path := "/" + s.token + "/send"
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"text":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	h := s.history
	if len(h) != 2 {
		t.Fatalf("history len = %d, want 2", len(h))
	}
	if h[0].Role != "user" || h[0].Text != "hi" {
		t.Errorf("history[0] = %+v, want user/hi", h[0])
	}
	if h[1].Role != "assistant" || h[1].Text != "hello world" {
		t.Errorf("history[1] = %+v, want assistant/hello world", h[1])
	}
}

func TestSendEmpty(t *testing.T) {
	s := New(stubEngine{}, Options{})
	rec := httptest.NewRecorder()
	path := "/" + s.token + "/send"
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"text":""}`))
	req.Header.Set("Content-Type", "application/json")
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSendWrongContentType(t *testing.T) {
	s := New(stubEngine{}, Options{})
	rec := httptest.NewRecorder()
	path := "/" + s.token + "/send"
	// form submission — classic CSRF vector
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("text=hi"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
}

func TestSendBadOrigin(t *testing.T) {
	s := New(stubEngine{}, Options{})
	rec := httptest.NewRecorder()
	path := "/" + s.token + "/send"
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"text":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	// cross-origin request — different host in Origin
	req.Header.Set("Origin", "http://evil.attacker.com")
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

type panicEngine struct{}

func (panicEngine) Send(context.Context, string, func(string)) (string, error) { panic("boom") }

func TestSendPanicReleasesBusy(t *testing.T) {
	s := New(panicEngine{}, Options{})
	req := httptest.NewRequest(http.MethodPost, "/"+s.token+"/send", strings.NewReader(`{"text":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	func() {
		defer func() { _ = recover() }()
		s.Handler().ServeHTTP(httptest.NewRecorder(), req)
	}()
	s.mu.Lock()
	busy := s.busy
	s.mu.Unlock()
	if busy {
		t.Fatal("busy stuck after engine panic")
	}
}

func TestServeShutdownWithOpenSSE(t *testing.T) {
	s := New(stubEngine{}, Options{Addr: "127.0.0.1:0"})
	ln, url, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()

	resp, err := http.Get(url + "events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve returned %v, want nil after clean shutdown", err)
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("shutdown took %v with an open SSE stream", d)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Serve did not return")
	}
}
