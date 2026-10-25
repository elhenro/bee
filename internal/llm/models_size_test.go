package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/elhenro/bee/internal/config"
)

func TestListModels_OmlxSizeFromStatus(t *testing.T) {
	ClearModelCache()
	ResetLiveContextLengths()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			fmt.Fprint(w, `{"data":[
				{"id":"cold","max_model_len":4096},
				{"id":"loaded","max_model_len":4096},
				{"id":"unknown","max_model_len":4096}
			]}`)
		case "/models/status":
			fmt.Fprint(w, `{"models":[
				{"id":"cold","estimated_size":16106127360,"actual_size":0},
				{"id":"loaded","estimated_size":16106127360,"actual_size":17179869184}
			]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	got, err := ListModels(context.Background(), "omlx", config.ProviderConfig{BaseURL: srv.URL, WireAPI: "chat"})
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	want := map[string]int64{"cold": 16106127360, "loaded": 17179869184, "unknown": 0}
	for _, m := range got {
		if m.SizeBytes != want[m.ID] {
			t.Errorf("%s SizeBytes = %d, want %d", m.ID, m.SizeBytes, want[m.ID])
		}
	}
}

func TestListModels_OllamaSizeFromTags(t *testing.T) {
	ClearModelCache()
	ResetLiveContextLengths()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			fmt.Fprint(w, `{"data":[{"id":"llama3:8b"},{"id":"qwen3:4b"}]}`)
		case "/api/tags":
			fmt.Fprint(w, `{"models":[{"name":"llama3:8b","size":4920000000}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	got, err := ListModels(context.Background(), "ollama", config.ProviderConfig{BaseURL: srv.URL + "/v1", WireAPI: "chat"})
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	want := map[string]int64{"llama3:8b": 4920000000, "qwen3:4b": 0}
	for _, m := range got {
		if m.SizeBytes != want[m.ID] {
			t.Errorf("%s SizeBytes = %d, want %d", m.ID, m.SizeBytes, want[m.ID])
		}
	}
}

// hosted providers never get the local size probes.
func TestListModels_HostedSkipsSizeProbe(t *testing.T) {
	ClearModelCache()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("unexpected probe %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"data":[{"id":"gpt-x","context_length":8192}]}`)
	}))
	defer srv.Close()

	got, err := ListModels(context.Background(), "openrouter", config.ProviderConfig{BaseURL: srv.URL, WireAPI: "chat"})
	if err != nil || len(got) != 1 || got[0].SizeBytes != 0 {
		t.Fatalf("got %+v err %v, want one model with SizeBytes 0", got, err)
	}
}
