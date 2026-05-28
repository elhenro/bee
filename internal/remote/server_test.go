package remote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

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

func TestSend(t *testing.T) {
	s := New(stubEngine{}, Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"text":"hi"}`))
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	h := s.History()
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
	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"text":""}`))
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
