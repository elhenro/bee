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

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/llm/mockprov"
)

// maxStdinBytes caps a message piped on stdin.
const maxStdinBytes = 1 << 20

func resolveUserMessage(positional []string, stdin io.Reader) (string, error) {
	if len(positional) > 0 {
		return strings.Join(positional, " "), nil
	}
	// tty stdin would block io.ReadFull until ^D; surface a clear error
	// instead of looking hung when run without args from an interactive shell.
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return "", fmt.Errorf("no user message: pass as args or pipe via stdin")
	}
	// stdin fallback. limit read so a stuck pipe doesn't hang forever; one
	// byte past the limit tells a cut-off message from one that fits exactly.
	buf, _ := io.ReadAll(io.LimitReader(stdin, maxStdinBytes+1))
	if len(buf) > maxStdinBytes {
		fmt.Fprintf(os.Stderr, "bee: stdin message truncated to %d bytes\n", maxStdinBytes)
		buf = buf[:maxStdinBytes]
	}
	s := strings.TrimSpace(string(buf))
	if s == "" {
		return "", fmt.Errorf("no user message: pass as args or stdin")
	}
	return s, nil
}

// resolveSafe maps --sandbox/--safe to a sandbox scope: an explicit --sandbox
// wins, then --safe means workspace-write, else def. Unattended surfaces (zzz,
// spawned agents) pass "workspace-write" as def so they confine by default.
func resolveSafe(sandboxScope string, safe bool, def string) string {
	switch {
	case sandboxScope != "":
		return sandboxScope
	case safe:
		return "workspace-write"
	}
	return def
}

// loadConfig loads config with the provider/model flags resolved up front (so
// the right API key applies), then pins the model, provider and
// sandbox scope. A test provider falls back to defaults so a missing key
// cannot break offline smoke runs.
func loadConfig(model, provider, sandboxScope string) (config.Config, error) {
	cfg, err := config.LoadWith(config.Overrides{Provider: provider, Model: model})
	if err != nil {
		if os.Getenv("BEE_TEST_PROVIDER") == "" {
			return cfg, err
		}
		cfg = config.Defaults()
	}
	if model != "" {
		cfg.DefaultModel = model
	}
	if provider != "" {
		cfg.DefaultProvider = provider
	}
	if sandboxScope != "" {
		cfg.Sandbox.Scope = sandboxScope
	}
	return cfg, nil
}

// mustConfig is loadConfig for a CLI entry point: a failure prints
// "<cmd>: config: ..." and exits 1.
func mustConfig(cmd, model, provider, sandboxScope string) config.Config {
	cfg, err := loadConfig(model, provider, sandboxScope)
	if err != nil {
		exitf(1, "%s: config: %v", cmd, err)
	}
	return cfg
}

// mustProvider is buildProvider for a CLI entry point: a failure prints
// "<cmd>: provider: ..." and exits 1.
func mustProvider(cmd string, cfg config.Config) llm.Provider {
	prov, err := buildProvider(cfg)
	if err != nil {
		exitf(1, "%s: provider: %v", cmd, err)
	}
	return prov
}

// applyThinkingCaveman applies --thinking, --effort (alias; the global flag
// lands in BEE_EFFORT as the lowest-priority source) and --caveman (global
// flag in BEE_CAVEMAN likewise). The caveman level is validated so a typo
// fails fast instead of silently falling back to the default.
func applyThinkingCaveman(cfg *config.Config, thinking, effort, cavemanLvl string) error {
	if effort == "" {
		effort = os.Getenv("BEE_EFFORT")
	}
	if thinking == "" {
		thinking = effort
	}
	if thinking != "" {
		cfg.Thinking = string(llm.ParseThinking(thinking))
	}
	if cavemanLvl == "" {
		cavemanLvl = os.Getenv("BEE_CAVEMAN")
	}
	if cavemanLvl == "" {
		return nil
	}
	lvl, err := caveman.ParseLevel(cavemanLvl)
	if err != nil {
		return err
	}
	cfg.Caveman = string(lvl)
	return nil
}

// localContextProbeTimeout bounds the one-time startup probe of a local
// server's context window. Generous because a loaded server can be slow, but
// a closed port fails fast on dial well before this.
const localContextProbeTimeout = 12 * time.Second

// resolveToolFormat maps cfg.ToolFormat to the effective mode.
// "" = auto: local providers default to "json" — every server bee labels
// local (ollama, llama.cpp, lmstudio, vllm, localai, omlx) compiles
// response_format json_schema to a sampling constraint in current versions,
// and openai_compat degrades gracefully (drops the constraint, keeps the
// JSON instruction) when an older build rejects the field. Hosted providers
// keep native tool_calls on auto. "native" forces native everywhere.
func resolveToolFormat(cfg config.Config) string {
	tf := cfg.ToolFormat
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
	// open the connection while the rest of startup (tools, skills, session)
	// runs, so the first request skips dial + TLS. no-op for loopback servers.
	if w, ok := inner.(llm.Prewarmer); ok {
		go w.Prewarm(context.Background())
	}
	switch resolveToolFormat(cfg) {
	case "xml":
		inner = llm.NewTextMode(inner, llm.TextModeOptions{})
	case "json":
		inner = llm.NewJSONMode(inner)
	}
	// prewarm: without a learned window the loop's budget falls back to a
	// useless 4*SystemPromptBudget.
	if config.IsLocalProvider(cfg.DefaultProvider) {
		prewarmLocalContext(cfg)
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
			RouteSort:          prov.RouteSort,
		}), nil
	case "gemini":
		// resolved for the selected provider, not cfg.APIKey: that one belongs
		// to the provider chosen at load and is stale after a switch or tier
		return llm.NewGemini(llm.GeminiConfig{
			BaseURL: prov.BaseURL,
			APIKey:  config.ProviderKey(cfg.DefaultProvider, prov),
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

// prewarmLocalContext learns the active local model's context window before
// the first turn so contextBudget starts from reality. Synchronous on purpose: a closed port fails fast on dial and a
// running server answers well under a second.
func prewarmLocalContext(cfg config.Config) {
	pc, ok := cfg.Providers[cfg.DefaultProvider]
	if !ok || pc.BaseURL == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), localContextProbeTimeout)
	defer cancel()

	// /api/show first: its num_ctx is what the runtime really allocates.
	// servers without it (omlx, lm-studio, vllm) 404 here, which is not a failure.
	if n, err := llm.ProbeOllamaContext(ctx, http.DefaultClient, pc.BaseURL, cfg.DefaultModel); err == nil && n > 0 {
		llm.RememberContextLength(cfg.DefaultModel, n)
		return
	}
	// those servers advertise the window on /models (context_length,
	// max_model_len) or /models/status, which ProbeContextLength reads.
	if llm.ProbeContextLength(ctx, cfg.DefaultProvider, pc, cfg.DefaultModel) > 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "bee: context window unknown for %s; using fallback budget\n", cfg.DefaultModel)
}
