package llm

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/elhenro/bee/internal/llm/wire"
)

// OpenAICompatConfig configures a chat-completions-style provider. The same
// implementation covers OpenRouter, OpenAI, DeepSeek, Groq, Ollama, LM Studio,
// Together, Fireworks — any service that speaks OpenAI's wire format.
type OpenAICompatConfig struct {
	// Name shows up in logs and Provider.Name(). Defaults to "openai-compat".
	Name string
	// BaseURL e.g. "https://openrouter.ai/api/v1". The "/chat/completions"
	// suffix is appended automatically.
	BaseURL string
	// EnvKey is the environment variable holding the bearer token. Optional —
	// when blank (local servers like Ollama), no Authorization header is sent.
	EnvKey string
	// HTTPClient is overridable for tests. Defaults to a long-timeout client.
	HTTPClient *http.Client
	// ChatTemplateKwargs flows into the MLX/vllm `chat_template_kwargs` body
	// field. Lets local servers flip Qwen3 template switches that change tool
	// emission shape. Nil = omit. Per-provider (TOML `chat_template_kwargs`).
	ChatTemplateKwargs map[string]any
	// ReportsCost opts the provider into a request flag asking for actual spend
	// in the usage block. Off by default; strict endpoints would reject the
	// extra field, so only set it for services that return real per-call cost.
	ReportsCost bool
	// KeepAlive sets Ollama's `keep_alive` body field (e.g. "15m") so the model
	// stays resident between turns instead of unloading after the server's idle
	// timeout — the next turn then skips a 2-10s reload before first token.
	// Empty = omit (default for hosted providers and MLX, which stay resident).
	KeepAlive string
	// NumCtx sets Ollama's runtime context window (num_ctx). >0 = explicit
	// operator override sent verbatim. 0 = auto: when KeepAlive marks this an
	// ollama-shaped server, allocate the probed window clamped to maxLocalNumCtx
	// so a small GPU isn't pushed to OOM by a 128k architectural max.
	NumCtx int
	// PromptCache opts the provider into emitting ephemeral cache_control
	// breakpoints on the system prefix (content-parts form). Only routers that
	// honor it (OpenRouter) should enable this; strict endpoints reject the
	// content-parts system message, and local servers cache by prefix anyway.
	PromptCache bool
	// RouteSort asks a routed aggregator to order upstreams by this key
	// ("price" | "throughput" | "latency"). Empty or "none" = omit and let the router pick.
	// Per-provider (TOML `route_sort`); only routers that honor it should set it.
	RouteSort string
}

// OpenAICompatProvider implements Provider against an OpenAI-compatible API.
type OpenAICompatProvider struct {
	cfg    OpenAICompatConfig
	client *http.Client
	// noTools caches models the provider has refused tools for (e.g. Ollama
	// returns 400 "does not support tools" for non-tool-tuned models). Once
	// observed, future requests for that model skip tools without paying the
	// failed-attempt round-trip.
	noTools sync.Map // model string -> struct{}
	// noRespFormat caches models whose server rejected response_format
	// (pre-grammar builds, strict proxies). The constraint is dropped and
	// remembered; jsonmode then runs prompt-instructed JSON without the
	// sampling guarantee instead of failing the turn.
	noRespFormat sync.Map // model string -> struct{}
}

// NewOpenAICompat builds a provider. Missing fields get sensible defaults.
func NewOpenAICompat(cfg OpenAICompatConfig) *OpenAICompatProvider {
	cfg.Name = cmp.Or(cfg.Name, "openai-compat")
	return &OpenAICompatProvider{cfg: cfg, client: cmp.Or(cfg.HTTPClient, newStreamingClient())}
}

// num_ctx auto-allocation bounds for ollama-shaped servers.
const (
	// ollamaDefaultNumCtx is ollama's built-in default. Below this there is
	// nothing to gain from sending the field.
	ollamaDefaultNumCtx = 4096
	// maxLocalNumCtx caps the auto value so a 128k architectural max doesn't
	// preallocate a KV cache that OOMs a small GPU (e.g. an 8GB Mac running a
	// 7-8B model). ~16k is a safe out-of-box headroom; operators wanting more
	// set an explicit num_ctx in provider config.
	maxLocalNumCtx = 16384
)

// resolveNumCtx decides the num_ctx to send. An explicit per-provider value
// wins for any provider. Otherwise auto-allocation fires only for ollama-shaped
// servers (those we keep warm) — MLX/vllm manage context server-side and may
// reject the field — clamping the probed window to maxLocalNumCtx. Returns 0
// to omit the field.
func (p *OpenAICompatProvider) resolveNumCtx(model string) int {
	if p.cfg.NumCtx > 0 {
		return p.cfg.NumCtx
	}
	if p.cfg.KeepAlive == "" {
		return 0
	}
	probed := ContextWindow(model)
	if probed <= ollamaDefaultNumCtx {
		return 0
	}
	if probed > maxLocalNumCtx {
		return maxLocalNumCtx
	}
	return probed
}

// isNoToolSupportError detects the Ollama/llama.cpp 400 returned for models
// that lack tool calling. Matched on substring so it survives Ollama version
// drift; the model id is interpolated into the message so we anchor on the
// suffix instead.
func isNoToolSupportError(code int, body []byte) bool {
	return badRequestMentions(code, body, "does not support tools", "model does not support tool")
}

// isNoResponseFormatError detects a 400 caused by the response_format field
// (server predates structured output or a strict proxy rejects unknown
// fields). Anchored on the field name so unrelated 400s never strip the
// constraint.
func isNoResponseFormatError(code int, body []byte) bool {
	return badRequestMentions(code, body, "response_format", "json_schema")
}

// badRequestMentions reports a 400 whose body contains any of subs, ignoring case.
func badRequestMentions(code int, body []byte, subs ...string) bool {
	s := strings.ToLower(string(body))
	return code == http.StatusBadRequest && slices.ContainsFunc(subs, func(sub string) bool { return strings.Contains(s, sub) })
}

// Name returns the configured display name.
func (p *OpenAICompatProvider) Name() string { return p.cfg.Name }

// Stream issues a chat completion and emits Events on the returned channel.
// Caller must read the channel until closed to avoid leaking the goroutine;
// ctx cancellation closes the channel after an EventError.
//
// Retries the pre-stream phase (DNS/TCP/TLS failures, 408, 429, 5xx) with
// exp-backoff. Once a 2xx response lands and streamLoop starts emitting, no
// further retries — replaying would duplicate tokens.
func (p *OpenAICompatProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	wireReq := p.buildWireRequest(req)
	if _, banned := p.noTools.Load(req.Model); banned {
		wireReq.Tools = nil
	}
	if _, banned := p.noRespFormat.Load(req.Model); banned {
		wireReq.ResponseFormat = nil
	}
	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/chat/completions"

	var resp *http.Response
	for resp == nil {
		body, err := json.Marshal(wireReq)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		r, err := postJSON(ctx, p.client, p.cfg.Name, url, body, func(r *http.Request) { p.applyHeaders(r, req.Stream) })
		if err != nil {
			return nil, err
		}
		if r.StatusCode < 400 {
			resp = r
			break
		}
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
		_ = r.Body.Close()
		switch {
		// some local servers reject tool advertisements for non-tool-tuned
		// models with 400 "does not support tools". strip tools, remember
		// the model, retry once.
		case len(wireReq.Tools) > 0 && isNoToolSupportError(r.StatusCode, raw):
			p.noTools.Store(req.Model, struct{}{})
			wireReq.Tools = nil
		// pre-grammar servers reject response_format. drop the constraint,
		// remember the model, retry — jsonmode degrades to prompt-instructed
		// JSON instead of failing the turn. warn once: the noRespFormat store
		// guarantees this branch runs at most once per model, so the stderr
		// line can't repeat.
		case wireReq.ResponseFormat != nil && isNoResponseFormatError(r.StatusCode, raw):
			fmt.Fprintf(os.Stderr, "bee: %s rejected response_format for %s; json tool mode continues without grammar enforcement\n", p.cfg.Name, req.Model)
			p.noRespFormat.Store(req.Model, struct{}{})
			wireReq.ResponseFormat = nil
		default:
			return nil, fmt.Errorf("provider %s status %d: %s", p.cfg.Name, r.StatusCode, raw)
		}
	}

	return startStream(ctx, resp, req.Stream, p.streamLoop, p.nonStreamLoop), nil
}

func (p *OpenAICompatProvider) buildWireRequest(req Request) wire.ChatRequest {
	wr := wire.BuildRequest(req.Model, req.System, req.Messages, toolAdverts(req.Tools), req.MaxTokens, req.Temperature, req.TopP, req.Stop, req.Stream)
	// opt in to provider-reported cost when the service supports it. gated so
	// strict endpoints never see the extra field.
	if p.cfg.ReportsCost {
		wr.Usage = &wire.UsageRequest{Include: true}
	}
	// cheapest-first upstream routing. gated so strict endpoints never see the
	// extra field.
	if p.cfg.RouteSort != "" && p.cfg.RouteSort != "none" {
		wr.Provider = &wire.ProviderRouting{Sort: p.cfg.RouteSort}
	}
	// OpenAI o-series + compatible: pass thinking level as reasoning_effort.
	// Omit on Off, and omit for models that don't honor the field — a non-
	// reasoning model (qwen3-coder, plain instruct) would otherwise receive an
	// ignored effort param just because the user left effort at medium. Qwen3
	// hybrids are excluded too: they take the /think token, not this wire field.
	if SupportsThinking(req.Model) {
		wr.ReasoningEffort = reasoningEffort(req.Thinking)
	}
	// keep local models warm between turns. gated so hosted/strict endpoints
	// never see the Ollama-specific field.
	wr.KeepAlive = p.cfg.KeepAlive
	// allocate the real context window on ollama-shaped servers. otherwise it
	// runs at the 4096 default and truncates the prompt head the loop budgeted
	// for. gated like keep_alive so hosted/strict endpoints never see the field.
	wr.NumCtx = p.resolveNumCtx(req.Model)
	// prompt-cache breakpoint on the static system prefix. gated to routers that
	// honor cache_control (OpenRouter); the static/dynamic split keeps the prefix
	// cached even when memory changes (see Request.SystemDynamic).
	if p.cfg.PromptCache {
		for i := range wr.Messages {
			if wr.Messages[i].Role != "system" {
				continue
			}
			if s, ok := wr.Messages[i].Content.(string); ok && s != "" {
				wr.Messages[i].Content = wire.SplitCachedSystem(s, req.SystemDynamic)
			}
			break
		}
	}
	// constrained output: jsonmode (and any future caller) sets ResponseSchema
	// to force the completion shape at sampling time on grammar-capable servers.
	if req.ResponseSchema != nil {
		wr.ResponseFormat = &wire.ResponseFormat{
			Type:       "json_schema",
			JSONSchema: &wire.JSONSchemaSpec{Name: "response", Strict: true, Schema: req.ResponseSchema},
		}
	}
	if len(p.cfg.ChatTemplateKwargs) > 0 || len(req.ChatTemplateKwargs) > 0 {
		merged := make(map[string]any, len(p.cfg.ChatTemplateKwargs)+len(req.ChatTemplateKwargs))
		maps.Copy(merged, p.cfg.ChatTemplateKwargs)
		maps.Copy(merged, req.ChatTemplateKwargs)
		wr.ChatTemplateKwargs = merged
	}
	return wr
}

// TODO: integration test against real OpenRouter, gated by BEE_E2E=1.
