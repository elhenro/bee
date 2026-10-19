package config

import "testing"

func TestIsLocalProvider(t *testing.T) {
	cases := map[string]bool{
		"ollama":     true,
		"lmstudio":   true,
		"llamacpp":   true,
		"llama-cpp":  true,
		"vllm":       true,
		"localai":    true,
		"omlx":       true,
		"OMLX":       true,
		"OLLAMA":     true,
		"openrouter": false,
		"openai":     false,
		"anthropic":  false,
		"":           false,
	}
	for in, want := range cases {
		if got := IsLocalProvider(in); got != want {
			t.Errorf("IsLocalProvider(%q) = %v, want %v", in, got, want)
		}
	}
}
