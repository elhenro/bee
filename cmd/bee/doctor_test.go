package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/config"
)

func TestStatusSym(t *testing.T) {
	cases := map[string]string{"ok": "✓", "warn": "!", "fail": "✗", "info": "·"}
	for in, want := range cases {
		if got := statusSym(in); got != want {
			t.Errorf("statusSym(%q) = %q want %q", in, got, want)
		}
	}
}

func TestExitCodeFor(t *testing.T) {
	if exitCodeFor([]check{{Status: "ok"}, {Status: "warn"}}) != 0 {
		t.Error("warn should not fail")
	}
	if exitCodeFor([]check{{Status: "ok"}, {Status: "fail"}}) != 1 {
		t.Error("fail should be 1")
	}
}

func TestTally(t *testing.T) {
	fail, warn, ok, info := tally([]check{{Status: "ok"}, {Status: "ok"}, {Status: "warn"}, {Status: "fail"}, {Status: "info"}})
	if fail != 1 || warn != 1 || ok != 2 || info != 1 {
		t.Errorf("tally mismatch: fail=%d warn=%d ok=%d info=%d", fail, warn, ok, info)
	}
}

func TestCheckSandboxHelper_runs(t *testing.T) {
	c := checkSandboxHelper()
	if c.Name != "sandbox helper" {
		t.Error("wrong name")
	}
	if c.Status == "" {
		t.Error("empty status")
	}
}

func ollamaCfg(baseURL, model string) config.Config {
	return config.Config{
		DefaultProvider: "ollama",
		DefaultModel:    model,
		Providers: map[string]config.ProviderConfig{
			"ollama": {BaseURL: baseURL + "/v1", WireAPI: "chat"},
		},
	}
}

func TestCheckOllama_NonOllamaProvider_NoChecks(t *testing.T) {
	cfg := config.Config{DefaultProvider: "openrouter"}
	if got := checkOllama(cfg); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestCheckOllama_UpModelPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tags":
			_, _ = io.WriteString(w, `{"models":[{"name":"qwen2.5-coder:7b","model":"qwen2.5-coder:7b"},{"name":"llama3.1:8b","model":"llama3.1:8b"}]}`)
		case "/api/show":
			_, _ = io.WriteString(w, `{"parameters":"num_ctx 32768","model_info":{}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	restore := setDoctorHTTPClient(&http.Client{Timeout: 2 * time.Second})
	defer restore()

	got := checkOllama(ollamaCfg(srv.URL, "qwen2.5-coder:7b"))
	if len(got) != 1 {
		t.Fatalf("len(checks) = %d, want 1: %+v", len(got), got)
	}
	if got[0].Status != "ok" {
		t.Errorf("status = %q, want ok (%+v)", got[0].Status, got[0])
	}
	if !strings.Contains(got[0].Detail, "num_ctx=32768") {
		t.Errorf("detail missing probed num_ctx: %q", got[0].Detail)
	}
}

func TestCheckOllama_UpModelMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"models":[{"name":"llama3.1:8b","model":"llama3.1:8b"}]}`)
	}))
	defer srv.Close()
	restore := setDoctorHTTPClient(&http.Client{Timeout: 2 * time.Second})
	defer restore()

	got := checkOllama(ollamaCfg(srv.URL, "qwen2.5-coder:7b"))
	if len(got) != 1 {
		t.Fatalf("len(checks) = %d, want 1", len(got))
	}
	if got[0].Status != "warn" {
		t.Errorf("status = %q, want warn", got[0].Status)
	}
	if !strings.Contains(got[0].Detail, "qwen2.5-coder:7b") || !strings.Contains(got[0].Detail, "not pulled") {
		t.Errorf("detail mismatch: %q", got[0].Detail)
	}
	if !strings.Contains(got[0].Remedy, "ollama pull qwen2.5-coder:7b") {
		t.Errorf("remedy mismatch: %q", got[0].Remedy)
	}
}

func TestCheckOllama_Down(t *testing.T) {
	restore := setDoctorHTTPClient(&http.Client{Timeout: 100 * time.Millisecond})
	defer restore()

	// 127.0.0.1:1 is reliably unreachable on every reasonable host.
	cfg := config.Config{
		DefaultProvider: "ollama",
		DefaultModel:    "qwen2.5-coder:7b",
		Providers: map[string]config.ProviderConfig{
			"ollama": {BaseURL: "http://127.0.0.1:1/v1", WireAPI: "chat"},
		},
	}
	got := checkOllama(cfg)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Status != "warn" {
		t.Errorf("status = %q, want warn", got[0].Status)
	}
	if !strings.Contains(got[0].Detail, "daemon not responding") {
		t.Errorf("detail mismatch: %q", got[0].Detail)
	}
}

func TestCheckOllama_DoesNotFailExitCode(t *testing.T) {
	// ensure WARN keeps exit 0 — the doctor must stay green when ollama is
	// merely unconfigured locally.
	cs := []check{
		{Status: "ok"},
		{Status: "warn", Detail: "daemon not responding"},
	}
	if got := exitCodeFor(cs); got != 0 {
		t.Errorf("exit = %d, want 0", got)
	}
}
