package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/elhenro/bee/internal/config"
)

func TestSupportsVision(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"claude-sonnet-4-6", true},
		{"claude-opus-4-7", true},
		{"anthropic/claude-haiku-4-5", true},
		{"gpt-4o", true},
		{"gpt-5.4", true},
		{"openai/o3", true},
		{"gemini-3-pro-preview", true},
		{"qwen3-vl-it", true},
		{"Qwen2.5-VL-7B", true},
		{"llava:13b", true},
		{"llama-3.2-11b-vision", true},
		// non-vision / text-only
		{"deepseek-v4-flash", false},
		{"deepseek-chat", false},
		{"qwen3-coder", false},
		{"llama-3.3-70b-versatile", false},
		{"", false},
	}
	for _, c := range cases {
		if got := SupportsVision(c.id); got != c.want {
			t.Errorf("SupportsVision(%q) = %v, want %v", c.id, got, c.want)
		}
	}
}

func TestSupportsVision_CatalogueOutranksNameList(t *testing.T) {
	ResetLiveVision()
	t.Cleanup(ResetLiveVision)
	RememberVision("z-ai/glm-5.3-flash", true)
	RememberVision("openai/gpt-5-text-only", false)
	if !SupportsVision("z-ai/glm-5.3-flash") {
		t.Error("catalogue says image input, want vision")
	}
	if SupportsVision("openai/gpt-5-text-only") {
		t.Error("catalogue says text only, want no vision despite name match")
	}
	if SupportsVision("z-ai/glm-5.3") {
		t.Error("unlearned unknown model must stay blind")
	}
}

func TestActiveModelHasVision_ProbesCatalogue(t *testing.T) {
	ResetLiveVision()
	ClearModelCache()
	t.Cleanup(func() { ResetLiveVision(); ClearModelCache() })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"z-ai/glm-5.3-flash","architecture":{"input_modalities":["text","image","video"]}},
			{"id":"z-ai/glm-5.3","architecture":{"input_modalities":["text"]}}]}`))
	}))
	defer srv.Close()
	cfg := &config.Config{
		DefaultProvider: "orx",
		DefaultModel:    "z-ai/glm-5.3-flash",
		Providers:       map[string]config.ProviderConfig{"orx": {BaseURL: srv.URL, WireAPI: "chat"}},
	}
	if !ActiveModelHasVision(context.Background(), cfg) {
		t.Fatal("glm-5.3-flash declares image input, want vision")
	}
	cfg.DefaultModel = "z-ai/glm-5.3"
	if ActiveModelHasVision(context.Background(), cfg) {
		t.Fatal("glm-5.3 is text only, want no vision")
	}
}

func TestActiveModelHasVision_UnreachableCatalogueStaysBlind(t *testing.T) {
	ResetLiveVision()
	ClearModelCache()
	t.Cleanup(func() { ResetLiveVision(); ClearModelCache() })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	cfg := &config.Config{
		DefaultProvider: "orx",
		DefaultModel:    "mystery-model",
		Providers:       map[string]config.ProviderConfig{"orx": {BaseURL: srv.URL, WireAPI: "chat"}},
	}
	if ActiveModelHasVision(context.Background(), cfg) {
		t.Fatal("no verdict available, want blind")
	}
}
