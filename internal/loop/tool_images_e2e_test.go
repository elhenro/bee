package loop

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/tools/read"
	"github.com/elhenro/bee/internal/types"
)

// readsPicture asks to read path on call one, then records what the model is
// shown on call two and answers.
type readsPicture struct {
	path string
	mu   sync.Mutex
	reqs []llm.Request
}

func (p *readsPicture) Name() string { return "peek" }
func (p *readsPicture) Stream(_ context.Context, req llm.Request) (<-chan llm.Event, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	n := len(p.reqs)
	p.mu.Unlock()
	ch := make(chan llm.Event, 3)
	go func() {
		defer close(ch)
		if n == 1 {
			ch <- llm.Event{Type: llm.EventToolUse, ToolUse: &types.ToolUse{
				ID: "r1", Name: "read", Input: map[string]any{"path": p.path},
			}}
		} else {
			ch <- llm.Event{Type: llm.EventTextDelta, Delta: "a blank square"}
		}
		ch <- llm.Event{Type: llm.EventDone}
	}()
	return ch, nil
}

func TestRunReadImageReachesModel(t *testing.T) {
	buf, reqs := runReadPicture(t, "claude-sonnet-4-6")
	last := reqs[1].Messages[len(reqs[1].Messages)-1]
	if last.Role != types.RoleTool || len(last.Content) < 2 {
		t.Fatalf("want a tool message with result + image, got %+v", last)
	}
	if last.Content[0].Type != types.BlockToolResult {
		t.Fatalf("tool_result must lead, got %s", last.Content[0].Type)
	}
	img := last.Content[len(last.Content)-1]
	if img.Type != types.BlockImage || img.MediaType != "image/png" || !bytes.Equal(img.Data, buf) {
		t.Fatalf("model not shown the picture: %+v", img)
	}
}

// a model without vision gets the fallback's text in the picture's place
func TestRunReadImageNonVisionModelGetsPlaceholder(t *testing.T) {
	_, reqs := runReadPicture(t, "deepseek-v4-flash")
	last := reqs[1].Messages[len(reqs[1].Messages)-1]
	img := last.Content[len(last.Content)-1]
	if img.Type != types.BlockText || !strings.Contains(img.Text, "image omitted") {
		t.Fatalf("want placeholder text, got %+v", img)
	}
}

func runReadPicture(t *testing.T, model string) ([]byte, []llm.Request) {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pic.png")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := tools.NewRegistry()
	if err := reg.Register(read.NewWithLimits(0, 0, true)); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
	cfg.Role = "worker"
	cfg.DefaultModel = model
	prov := &readsPicture{path: path}
	eng := &Engine{Provider: prov, Tools: reg, Memory: stubMemStore{}, Cfg: cfg, Cwd: ".", Stdout: io.Discard}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := eng.Run(ctx, "what is in "+path); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(prov.reqs) != 2 {
		t.Fatalf("want 2 provider calls, got %d", len(prov.reqs))
	}
	return buf.Bytes(), prov.reqs
}
