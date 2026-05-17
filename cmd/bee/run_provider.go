// Provider construction and input plumbing for `bee run`.
// Wire-api routing, test-stub short-circuits, prewarm probes, stdin/flag
// resolution.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/llm/mockprov"
)

func resolveUserMessage(positional []string, stdin io.Reader) (string, error) {
	if len(positional) > 0 {
		return strings.Join(positional, " "), nil
	}
	// tty stdin would block io.ReadFull until ^D; surface a clear error
	// instead of looking hung when run without args from an interactive shell.
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return "", fmt.Errorf("no user message: pass as args or pipe via stdin")
	}
	// stdin fallback. limit read so a stuck pipe doesn't hang forever.
	buf := make([]byte, 1<<20)
	n, _ := io.ReadFull(stdin, buf)
	s := strings.TrimSpace(string(buf[:n]))
	if s == "" {
		return "", fmt.Errorf("no user message: pass as args or stdin")
	}
	return s, nil
}

func applyOverrides(cfg *config.Config, model, provName, sandboxScope string) {
	if model != "" {
		cfg.DefaultModel = model
	}
	if provName != "" {
		cfg.DefaultProvider = provName
	}
	if sandboxScope != "" {
		cfg.Sandbox.Scope = sandboxScope
	}
}

func buildProvider(cfg config.Config) (llm.Provider, error) {
	inner, err := buildProviderInner(cfg)
	if err != nil {
		return nil, err
	}
	// XML/text-mode wrap: active profile opts in via ToolFormat="xml". Useful
	// for small local models that ignore native tool_calls (llama3.1:8b,
	// gemma3, phi3). Default "" keeps native tool calls.
	if config.ActiveProfile(cfg).ToolFormat == "xml" {
		inner = llm.NewTextMode(inner, llm.TextModeOptions{})
	}
	// prewarm: local providers don't expose context_length on /v1/models, so
	// the loop's budget falls back to a useless 4*SystemPromptBudget. Fire a
	// best-effort /api/show probe in the background and stash the answer in
	// the context cache so contextBudget reflects reality from turn one.
	if config.IsLocalProvider(cfg.DefaultProvider) {
		if pc, ok := cfg.Providers[cfg.DefaultProvider]; ok && pc.BaseURL != "" {
			go func(baseURL, modelID string) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if n, err := llm.ProbeOllamaContext(ctx, http.DefaultClient, baseURL, modelID); err == nil && n > 0 {
					llm.RememberContextLength(modelID, n)
				}
			}(pc.BaseURL, cfg.DefaultModel)
		}
	}
	return inner, nil
}

func buildProviderInner(cfg config.Config) (llm.Provider, error) {
	// test stub short-circuit: deterministic responses, no network.
	switch os.Getenv("BEE_TEST_PROVIDER") {
	case "stub":
		return newStubProvider(), nil
	case "scripted":
		path := os.Getenv("BEE_TEST_SCRIPT")
		if path == "" {
			return nil, fmt.Errorf("BEE_TEST_PROVIDER=scripted requires BEE_TEST_SCRIPT=<fixture path>")
		}
		f, err := mockprov.Load(path)
		if err != nil {
			return nil, err
		}
		return mockprov.NewScripted(f), nil
	}
	prov, ok := cfg.Providers[cfg.DefaultProvider]
	if !ok {
		return nil, fmt.Errorf("provider %q not configured", cfg.DefaultProvider)
	}
	// route by wire_api: chat → openai-compat, gemini → native, responses →
	// chatgpt-subscription backend, anything else falls through as unsupported.
	switch prov.WireAPI {
	case "", "chat":
		return llm.NewOpenAICompat(llm.OpenAICompatConfig{
			Name:    cfg.DefaultProvider,
			BaseURL: prov.BaseURL,
			EnvKey:  prov.EnvKey,
		}), nil
	case "gemini":
		key := cfg.APIKey
		if key == "" && prov.EnvKey != "" {
			key = os.Getenv(prov.EnvKey)
		}
		return llm.NewGemini(llm.GeminiConfig{
			BaseURL: prov.BaseURL,
			APIKey:  key,
		}), nil
	case "responses":
		cgCfg := llm.ChatGPTConfig{
			Name:    cfg.DefaultProvider,
			BaseURL: prov.BaseURL,
		}
		if prov.OAuth != nil {
			cgCfg.ClientID = prov.OAuth.ClientID
			cgCfg.TokenEndpoint = prov.OAuth.TokenEndpoint
			cgCfg.AccountIDHeader = prov.OAuth.AccountIDHeader
		}
		return llm.NewChatGPT(cgCfg), nil
	case "anthropic-messages":
		return llm.NewClaude(llm.ClaudeConfig{
			Name:    cfg.DefaultProvider,
			BaseURL: prov.BaseURL,
			EnvKey:  prov.EnvKey,
		}), nil
	default:
		return nil, fmt.Errorf("wire_api %q not supported yet", prov.WireAPI)
	}
}
