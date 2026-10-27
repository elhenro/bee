package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryAfterParse(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	cases := []struct {
		name, val string
		want      time.Duration
	}{
		{"absent", "", 0},
		{"seconds", "7", 7 * time.Second},
		{"capped", "3600", retryAfterCap},
		{"negative", "-4", 0},
		{"garbage", "soon", 0},
		{"date", now.Add(12 * time.Second).Format(http.TimeFormat), 12 * time.Second},
		{"date past", now.Add(-time.Minute).Format(http.TimeFormat), 0},
		{"date capped", now.Add(time.Hour).Format(http.TimeFormat), retryAfterCap},
	}
	for _, c := range cases {
		h := http.Header{}
		if c.val != "" {
			h.Set("Retry-After", c.val)
		}
		if got := retryAfter(h, now); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPostWithRetryHonorsRetryAfter(t *testing.T) {
	fastRetry(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	start := time.Now()
	resp, err := postJSON(context.Background(), srv.Client(), "x", srv.URL, []byte(`{}`), func(*http.Request) {})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if hits.Load() != 2 {
		t.Fatalf("hits = %d, want 2", hits.Load())
	}
	if el := time.Since(start); el < 900*time.Millisecond {
		t.Errorf("retried after %v, want >= ~1s from Retry-After", el)
	}
}

func TestPostWithRetryRetryAfterRespectsCtx(t *testing.T) {
	fastRetry(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := postJSON(ctx, srv.Client(), "x", srv.URL, []byte(`{}`), func(*http.Request) {}); err == nil {
		t.Fatal("want ctx error")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("ctx cancel must cut the Retry-After wait")
	}
}
