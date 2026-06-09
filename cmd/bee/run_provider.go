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

// resolveSafe maps the --safe shorthand to a sandbox scope. An explicit
// --sandbox always wins; --safe only applies when no scope was given.
func resolveSafe(sandboxScope string, safe bool) string {
	if sandboxScope == "" && safe {
		return "workspace-write"
	}
	return sandboxScope
}

// resolveSafeDefault is resolveSafe but falls back to def when the user gave
// neither --sandbox nor --safe. Unattended surfaces (zzz, spawned agents) pass
// "workspace-write" so they confine by default; an explicit --sandbox (incl.
// danger-full-access) or --safe still wins.
func resolveSafeDefault(sandboxScope string, safe bool, def string) string {
	if s := resolveSafe(sandboxScope, safe); s != "" {
		return s
	}
	return def
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

// localContextProbeTimeout bounds the one-time startup probe of a local
// server's context window. Generous because a loaded server can be slow, but
// a closed port fails fast on dial well before this.
const localContextProbeTimeout = 12 * time.Second

// resolveToolFormat maps the profile's ToolFormat to the effective mode.
// "" = auto: local providers default to "json" — every server bee labels
// local (ollama, llama.cpp, lmstudio, vllm, localai, omlx) compiles
// response_format json_schema to a sampling constraint in current versions,
// and openai_compat degrades gracefully (drops the constraint, keeps the
// JSON instruction) when an older build rejects the field. Hosted providers
// keep native tool_calls on auto. "native" forces native everywhere.
func resolveToolFormat(cfg config.Config) string {
	tf := config.ActiveProfile(cfg).ToolFormat
	if tf == "" && config.IsLocalProvider(cfg.DefaultProvider) {
		return "json"
	}
	return tf
}

func buildProvider(cfg config.Config) (llm.Provider, error) {
	inner, err := buildProviderInner(cfg)
	if err != nil {
		return nil, err
	}
	// tool-format wrap. "xml" = TextModeProvider (text advert + tag parsing,
	// for models that ignore native tool_calls). "json" = JSONModeProvider:
	// grammar-constrained JSON via response_format json_schema — malformed
	// calls become impossible on servers that compile the schema to a
	// sampling constraint. "" auto-resolves (json on local providers);
	// "native" forces the native tool_calls channel.
	switch resolveToolFormat(cfg) {
	case "xml":
		inner = llm.NewTextMode(inner, llm.TextModeOptions{})
	case "json":
		inner = llm.NewJSONMode(inner)
	}
	// prewarm: local providers don't expose context_length on /v1/models, so
	// the loop's budget falls back to a useless 4*SystemPromptBudget. Probe
	// /api/show synchronously (a closed port fails fast on dial; a running
	// server answers in well under a second) so contextBudget — and the
	// ToolOutputTokens / SystemPromptBudget scaling in ScaleProfileForContext —
	// reflect reality from turn one instead of after the probe loses the race.
	if config.IsLocalProvider(cfg.DefaultProvider) {
		if pc, ok := cfg.Providers[cfg.DefaultProvider]; ok && pc.BaseURL != "" {
			ctx, cancel := context.WithTimeout(context.Background(), localContextProbeTimeout)
			if n, err := llm.ProbeOllamaContext(ctx, http.DefaultClient, pc.BaseURL, cfg.DefaultModel); err == nil && n > 0 {
				llm.RememberContextLength(cfg.DefaultModel, n)
			} else if err != nil {
				fmt.Fprintf(os.Stderr, "bee: local context probe failed (%v); using fallback budget\n", err)
			}
			cancel()
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
			Name:               cfg.DefaultProvider,
			BaseURL:            prov.BaseURL,
			EnvKey:             prov.EnvKey,
			ChatTemplateKwargs: prov.ChatTemplateKwargs,
			ReportsCost:        prov.ReportsCost,
			KeepAlive:          prov.KeepAlive,
			PromptCache:        prov.SupportsPromptCache,
			NumCtx:             prov.NumCtx,
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
