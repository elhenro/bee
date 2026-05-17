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

func TestIsLoopbackURL(t *testing.T) {
	cases := map[string]bool{
		"http://localhost:11434":  true,
		"http://127.0.0.1:1234":   true,
		"http://[::1]:8080":       true,
		"https://localhost":       true,
		"http://10.0.0.1:8080":    false,
		"https://api.openai.com":  false,
		"http://example.com:8080": false,
		"":                        false,
		"not a url at all":        false,
	}
	for in, want := range cases {
		if got := IsLoopbackURL(in); got != want {
			t.Errorf("IsLoopbackURL(%q) = %v, want %v", in, got, want)
		}
	}
}
