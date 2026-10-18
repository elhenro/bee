package arena

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestParseUsageExtractsTokens(t *testing.T) {
	in, out, ok := parseUsage([]byte(`{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20}}`))
	if !ok {
		t.Fatal("usage not parsed")
	}
	if in != 100 || out != 20 {
		t.Fatalf("parsed (%d,%d), want (100,20)", in, out)
	}
}

func TestParseUsageMissingIsNotOK(t *testing.T) {
	if _, _, ok := parseUsage([]byte(`{"choices":[]}`)); ok {
		t.Fatal("missing usage reported ok=true")
	}
}

func newUpstream(t *testing.T, sawBody *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if sawBody != nil {
			*sawBody = string(b)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"text":"hi"}],"usage":{"prompt_tokens":100,"completion_tokens":20}}`)
	}))
}

func TestProxyForwardsAndMetersWallet(t *testing.T) {
	up := newUpstream(t, nil)
	defer up.Close()
	upURL, _ := url.Parse(up.URL)
	w := NewWallet(200_000)
	cost := DefaultCostSchedule()
	px := NewMeteringProxy(&Side{Name: "red", Upstream: upURL, Wallet: w, Cost: cost})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/red/v1/chat/completions", strings.NewReader(`{"model":"m"}`))
	px.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "choices") {
		t.Fatalf("upstream body not forwarded: %q", rec.Body.String())
	}
	wantDebit := cost.MeterUsage(100, 20)
	if w.Spent != wantDebit {
		t.Fatalf("wallet Spent = %d, want %d (metered from usage)", w.Spent, wantDebit)
	}
	if w.Balance != 200_000-wantDebit {
		t.Fatalf("wallet Balance = %d, want %d", w.Balance, 200_000-wantDebit)
	}
}

func TestProxyRejectsBankruptSideWith402(t *testing.T) {
	var called bool
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		io.WriteString(w, "{}")
	}))
	defer up.Close()
	upURL, _ := url.Parse(up.URL)
	px := NewMeteringProxy(&Side{Name: "blue", Upstream: upURL, Wallet: NewWallet(0), Cost: DefaultCostSchedule()})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/blue/v1/chat/completions", strings.NewReader(`{}`))
	px.ServeHTTP(rec, req)

	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want 402", rec.Code)
	}
	if called {
		t.Fatal("upstream must not be called for a bankrupt side")
	}
}

func TestProxyRewritesModelForAsymmetricRouting(t *testing.T) {
	var sawBody string
	up := newUpstream(t, &sawBody)
	defer up.Close()
	upURL, _ := url.Parse(up.URL)
	px := NewMeteringProxy(&Side{Name: "red", Upstream: upURL, Model: "claude-opus-4-8", Wallet: NewWallet(99999), Cost: DefaultCostSchedule()})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/red/v1/chat/completions", strings.NewReader(`{"model":"placeholder"}`))
	px.ServeHTTP(rec, req)

	if !strings.Contains(sawBody, "claude-opus-4-8") {
		t.Fatalf("upstream did not see rewritten model; saw %q", sawBody)
	}
}

func TestProxyUnknownSideIs404(t *testing.T) {
	px := NewMeteringProxy(&Side{Name: "red", Upstream: &url.URL{}, Wallet: NewWallet(1), Cost: DefaultCostSchedule()})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/green/v1/x", strings.NewReader(``))
	px.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown side status = %d, want 404", rec.Code)
	}
}

func TestParseUsageReadsSSEFinalChunk(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3}}\n\n" +
		"data: [DONE]\n\n"
	in, out, ok := parseUsage([]byte(body))
	if !ok || in != 7 || out != 3 {
		t.Fatalf("in=%d out=%d ok=%v", in, out, ok)
	}
}

func TestProxyMetersStreamedResponse(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20}}\n\ndata: [DONE]\n\n")
	}))
	defer up.Close()
	upURL, _ := url.Parse(up.URL)
	w := NewWallet(200_000)
	cost := DefaultCostSchedule()
	px := NewMeteringProxy(&Side{Name: "red", Upstream: upURL, Wallet: w, Cost: cost})
	rec := httptest.NewRecorder()
	px.ServeHTTP(rec, httptest.NewRequest("POST", "/red/v1/chat/completions", strings.NewReader(`{"stream":true}`)))
	if !strings.Contains(rec.Body.String(), "[DONE]") {
		t.Fatalf("stream not forwarded: %q", rec.Body.String())
	}
	if want := cost.MeterUsage(100, 20); w.Spent != want {
		t.Fatalf("Spent = %d, want %d", w.Spent, want)
	}
}
