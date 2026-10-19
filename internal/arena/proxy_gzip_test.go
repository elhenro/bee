package arena

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// a Go client sends Accept-Encoding: gzip; a compressing upstream must still
// be metered and the client must still get a readable body.
func TestProxyMetersGzipUpstream(t *testing.T) {
	const body = `{"choices":[],"usage":{"prompt_tokens":1000,"completion_tokens":500}}`
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			_, _ = gz.Write([]byte(body))
			_ = gz.Close()
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	defer up.Close()
	u, _ := url.Parse(up.URL)
	wal := NewWallet(100_000)
	px := httptest.NewServer(NewMeteringProxy(&Side{Name: "red", Upstream: u, Wallet: wal, Cost: DefaultCostSchedule()}))
	defer px.Close()

	resp, err := http.Post(px.URL+"/red/v1/chat/completions", "application/json", strings.NewReader(`{"model":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if string(got) != body {
		t.Fatalf("client body = %q", got)
	}
	if want := DefaultCostSchedule().MeterUsage(1000, 500); wal.Spent != want {
		t.Fatalf("spent %d, want %d", wal.Spent, want)
	}
}
