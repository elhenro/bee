// Package tools defines the Tool interface and an in-memory registry.
//
// Concrete tools (apply_patch, shell, read) live in sub-packages and register
// themselves with a Registry held by the main loop.
package tools

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/elhenro/bee/internal/llm"
)

// Tool is the contract every executable tool must satisfy.
type Tool interface {
	Spec() llm.ToolSpec
	Run(ctx context.Context, input map[string]any) (Result, error)
}

// Result is the tool output handed back to the model.
type Result struct {
	Content string
	IsError bool
}

// Registry maps tool name -> Tool. Safe for concurrent reads after build.
type Registry struct {
	mu         sync.RWMutex
	tools      map[string]Tool
	specsCache []llm.ToolSpec // sorted snapshot; nil = stale, rebuilt on next Specs
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := t.Spec().Name
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %q already registered", name)
	}
	r.tools[name] = t
	r.specsCache = nil // invalidate sorted snapshot
	return nil
}

// Without returns a new Registry holding every tool except the named ones.
// Used to hand observation-only agents (e.g. the queen's review gate) a surface
// with the structured write tools removed, so an accidental edit-call fails
// instead of mutating the tree mid-review. The excluded set is small, so a
// linear skip-check is fine.
func (r *Registry) Without(exclude ...string) *Registry {
	skip := make(map[string]struct{}, len(exclude))
	for _, n := range exclude {
		skip[n] = struct{}{}
	}
	out := NewRegistry()
	r.mu.RLock()
	defer r.mu.RUnlock()
	for name, t := range r.tools {
		if _, drop := skip[name]; drop {
			continue
		}
		out.tools[name] = t
	}
	return out
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Names returns the registered tool names sorted alphabetically. Used by
// diagnostics (e.g. unknown-tool error feedback) so the model can recover
// without having to re-read the system prompt.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.tools))
	for n := range r.tools {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Specs returns every registered tool's spec, sorted alphabetically by name.
// The sort guarantees a stable order across calls and process runs — critical
// for KV-cache prefix hits on the system prompt's tool manifest, which would
// otherwise reshuffle on every turn (Go map iteration is randomized).
//
// The sorted slice is cached and only rebuilt after a Register, so the per-turn
// O(N log N) sort runs once per session instead of on every turn. Callers get a
// fresh copy each call; the pipeline downstream only ever reads or rebuilds into
// new slices (never mutates a ToolSpec in place), so the snapshot stays clean.
func (r *Registry) Specs() []llm.ToolSpec {
	r.mu.RLock()
	if r.specsCache != nil {
		out := append([]llm.ToolSpec(nil), r.specsCache...)
		r.mu.RUnlock()
		return out
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.specsCache == nil {
		built := make([]llm.ToolSpec, 0, len(r.tools))
		for _, t := range r.tools {
			built = append(built, t.Spec())
		}
		sort.Slice(built, func(i, j int) bool { return built[i].Name < built[j].Name })
		r.specsCache = built
	}
	return append([]llm.ToolSpec(nil), r.specsCache...)
}
