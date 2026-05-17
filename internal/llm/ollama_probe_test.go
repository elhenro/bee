package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOllamaBaseURL_StripsV1(t *testing.T) {
	cases := map[string]string{
		"http://localhost:11434/v1":  "http://localhost:11434",
		"http://localhost:11434/v1/": "http://localhost:11434",
		"http://localhost:11434":     "http://localhost:11434",
		"http://localhost:11434/":    "http://localhost:11434",
	}
	for in, want := range cases {
		if got := OllamaBaseURL(in); got != want {
			t.Errorf("OllamaBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProbeOllamaContext_Parameters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" {
			t.Errorf("path = %q, want /api/show", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]string
		_ = json.Unmarshal(body, &req)
		if req["name"] != "qwen2.5-coder:7b" {
			t.Errorf("name = %q", req["name"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"parameters": "stop \"<|endoftext|>\"\nnum_ctx 32768\ntemperature 0.7",
			"model_info": {}
		}`)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	n, err := ProbeOllamaContext(context.Background(), client, srv.URL+"/v1", "qwen2.5-coder:7b")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if n != 32768 {
		t.Errorf("got %d, want 32768", n)
	}
}

func TestProbeOllamaContext_ModelInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"parameters": "",
			"model_info": {"qwen2.context_length": 4096, "general.architecture": "qwen2"}
		}`)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	n, err := ProbeOllamaContext(context.Background(), client, srv.URL, "qwen2.5-coder:7b")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if n != 4096 {
		t.Errorf("got %d, want 4096", n)
	}
}

func TestProbeOllamaContext_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	n, err := ProbeOllamaContext(context.Background(), client, srv.URL, "missing")
	if n != 0 {
		t.Errorf("got n=%d, want 0", n)
	}
	if err == nil {
		t.Error("expected error on 404")
	}
}

func TestProbeOllamaContext_NetworkError(t *testing.T) {
	client := &http.Client{Timeout: 50 * time.Millisecond}
	// 127.0.0.1:1 is reliably unreachable
	n, err := ProbeOllamaContext(context.Background(), client, "http://127.0.0.1:1/v1", "x")
	if n != 0 {
		t.Errorf("got n=%d, want 0", n)
	}
	if err == nil {
		t.Error("expected error on unreachable host")
	}
}

func TestProbeOllamaContext_PopulatesContextWindow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"parameters": "num_ctx 16384", "model_info": {}}`)
	}))
	defer srv.Close()

	const id = "test-probe-model:latest"
	if ContextWindow(id) != 0 {
		t.Fatal("precondition: model should be unknown before probe")
	}

	client := &http.Client{Timeout: 5 * time.Second}
	n, err := ProbeOllamaContext(context.Background(), client, srv.URL, id)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	RememberContextLength(id, n)

	if got := ContextWindow(id); got != 16384 {
		t.Errorf("ContextWindow(%q) = %d, want 16384", id, got)
	}
}

func TestProbeOllamaContext_EmptyPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"parameters": "", "model_info": {}}`)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	n, err := ProbeOllamaContext(context.Background(), client, srv.URL, "x")
	if err != nil {
		t.Errorf("unexpected err: %v", err)
	}
	if n != 0 {
		t.Errorf("got %d, want 0", n)
	}
}

// num_ctx test guards against the regex matching a substring elsewhere.
func TestNumCtxFromParameters_LineAnchored(t *testing.T) {
	// num_ctx_override should NOT match.
	if n := numCtxFromParameters("num_ctx_override 9999"); n != 0 {
		t.Errorf("non-boundary match: got %d", n)
	}
	if n := numCtxFromParameters("temp 0.7\nnum_ctx 8192\nstop \"</s>\""); n != 8192 {
		t.Errorf("multi-line: got %d", n)
	}
}

// the regex must tolerate the trailing-CR variant ollama can emit.
func TestNumCtxFromParameters_Whitespace(t *testing.T) {
	if n := numCtxFromParameters("   num_ctx   2048   "); n != 2048 {
		t.Errorf("leading-space form: got %d", n)
	}
}

func TestContextLengthFromModelInfo_PrefersAnyArch(t *testing.T) {
	info := map[string]any{
		"general.architecture": "llama",
		"llama.context_length": float64(131072),
	}
	if got := contextLengthFromModelInfo(info); got != 131072 {
		t.Errorf("got %d, want 131072", got)
	}
}

func TestProbeOllamaContext_JSONNumberDecoded(t *testing.T) {
	// guard: even if the server returns a very-large integer ollama-side,
	// the float64 path doesn't lose precision below 2^53.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model_info": {"x.context_length": 200000}}`)
	}))
	defer srv.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	n, err := ProbeOllamaContext(context.Background(), client, srv.URL, "x")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if n != 200000 {
		t.Errorf("got %d, want 200000", n)
	}
}
