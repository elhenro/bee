// Package tools defines the Tool interface and an in-memory registry.
//
// Concrete tools (apply_patch, shell, read) live in sub-packages and register
// themselves with a Registry held by the main loop.
package tools

import (
	"context"
	"fmt"
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
	mu    sync.RWMutex
	tools map[string]Tool
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
	return nil
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) Specs() []llm.ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]llm.ToolSpec, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t.Spec())
	}
	return out
}
