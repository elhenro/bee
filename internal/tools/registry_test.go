package tools

import (
	"context"
	"sync"
	"testing"

	"github.com/elhenro/bee/internal/llm"
)

type _stubTool struct {
	name string
}

func (t *_stubTool) Spec() llm.ToolSpec { return llm.ToolSpec{Name: t.name} }
func (t *_stubTool) Run(_ context.Context, _ map[string]any) (Result, error) {
	return Result{Content: "ok"}, nil
}

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if r.tools == nil {
		t.Fatal("Registry.tools is nil")
	}
	if len(r.tools) != 0 {
		t.Errorf("new registry has %d tools, want 0", len(r.tools))
	}
}

func TestRegister_Get(t *testing.T) {
	r := NewRegistry()
	tool := &_stubTool{name: "bash"}
	if err := r.Register(tool); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, ok := r.Get("bash")
	if !ok {
		t.Fatal("Get(\"bash\") not found")
	}
	if got.Spec().Name != "bash" {
		t.Errorf("Get returned tool %q, want \"bash\"", got.Spec().Name)
	}

	_, ok = r.Get("nonexistent")
	if ok {
		t.Error("Get(\"nonexistent\") returned ok=true, want false")
	}
}

func TestRegister_Duplicate(t *testing.T) {
	r := NewRegistry()
	tool1 := &_stubTool{name: "bash"}
	tool2 := &_stubTool{name: "bash"}

	if err := r.Register(tool1); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := r.Register(tool2)
	if err == nil {
		t.Error("expected error registering duplicate, got nil")
	}
	if err == nil || err.Error() == "" {
		t.Error("duplicate error should describe the problem")
	}

	// first tool still accessible
	got, ok := r.Get("bash")
	if !ok || got.Spec().Name != "bash" {
		t.Error("original tool should still be accessible after duplicate rejection")
	}
}

func TestSpecs(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&_stubTool{name: "bash"})
	_ = r.Register(&_stubTool{name: "read"})
	_ = r.Register(&_stubTool{name: "write"})

	specs := r.Specs()
	if len(specs) != 3 {
		t.Fatalf("Specs returned %d, want 3", len(specs))
	}
	names := map[string]bool{}
	for _, s := range specs {
		names[s.Name] = true
	}
	for _, want := range []string{"bash", "read", "write"} {
		if !names[want] {
			t.Errorf("Specs missing %q", want)
		}
	}
}

func TestSpecs_Empty(t *testing.T) {
	if got := NewRegistry().Specs(); len(got) != 0 {
		t.Errorf("empty registry Specs = %d, want 0", len(got))
	}
}

func TestRegistry_Concurrent(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup

	// 10 goroutines register different tools
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := "tool_" + string(rune('a'+idx))
			_ = r.Register(&_stubTool{name: name})
		}(i)
	}

	// 10 goroutines read specs concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.Specs()
			_, _ = r.Get("tool_0")
		}()
	}

	wg.Wait()
	specs := r.Specs()
	if len(specs) != 10 {
		t.Errorf("concurrent register: %d tools, want 10", len(specs))
	}
}
