package web_fetch

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestFetchURL_ErrorStatusNotCached(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			http.Error(w, "temporarily down", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("all good"))
	}))
	defer srv.Close()

	r1, err := FetchURL(srv.URL+"/flaky", nil, false)
	if err != nil || r1.Code != http.StatusServiceUnavailable {
		t.Fatalf("first fetch: code=%v err=%v", r1, err)
	}
	r2, err := FetchURL(srv.URL+"/flaky", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Code != http.StatusOK || r2.Markdown != "all good" {
		t.Errorf("recovered server still served stale error: code=%d body=%q", r2.Code, r2.Markdown)
	}
}

func TestFetchURL_SuccessCached(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	for i := 0; i < 2; i++ {
		if _, err := FetchURL(srv.URL+"/stable", nil, false); err != nil {
			t.Fatal(err)
		}
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Errorf("2xx not cached: server hit %d times", n)
	}
}
