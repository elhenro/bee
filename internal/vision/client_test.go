package vision

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDescribe_OpenAI(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "a red button"}}},
		})
	}))
	defer srv.Close()

	c := Client{Model: "qwen-vl", Endpoint: srv.URL, APIKey: "secret", API: "openai"}
	out, err := c.Describe(context.Background(), []byte("png-bytes"), "image/png", "")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if out != "a red button" {
		t.Errorf("out = %q", out)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("auth = %q", gotAuth)
	}
	if !strings.Contains(gotBody, "data:image/png;base64,") {
		t.Errorf("body missing data url: %q", gotBody)
	}
}

func TestDescribe_Ollama(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]string{"response": "a login form"})
	}))
	defer srv.Close()

	c := Client{Model: "llava", Endpoint: srv.URL, API: "ollama"}
	out, err := c.Describe(context.Background(), []byte("png"), "image/png", "what is this?")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if out != "a login form" {
		t.Errorf("out = %q", out)
	}
	if gotPath != "/api/generate" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestDescribe_RequiresModelAndEndpoint(t *testing.T) {
	if _, err := (Client{Endpoint: "x"}).Describe(context.Background(), nil, "", ""); err == nil {
		t.Error("expected error with empty model")
	}
	if _, err := (Client{Model: "m"}).Describe(context.Background(), nil, "", ""); err == nil {
		t.Error("expected error with empty endpoint")
	}
}

func TestDescribe_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := Client{Model: "m", Endpoint: srv.URL}
	if _, err := c.Describe(context.Background(), []byte("x"), "", ""); err == nil {
		t.Error("expected error on 500")
	}
}
