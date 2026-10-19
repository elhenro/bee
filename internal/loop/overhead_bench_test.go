package loop

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// instantProvider answers every call with one read tool call, then a final
// text after n calls. zero model latency, so wall time is pure harness cost.
type instantProvider struct {
	n     int32
	calls atomic.Int32
}

func (p *instantProvider) Name() string { return "instant" }

func (p *instantProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	i := p.calls.Add(1)
	ch := make(chan llm.Event, 4)
	usage := &llm.Usage{InputTokens: 5000, OutputTokens: 40}
	if i > p.n {
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: "done"}
		ch <- llm.Event{Type: llm.EventDone, Usage: usage}
	} else {
		ch <- llm.Event{Type: llm.EventToolUse, ToolUse: &types.ToolUse{
			ID: fmt.Sprintf("call_%d", i), Name: "read",
			Input: map[string]any{"path": fmt.Sprintf("f%d.go", i)},
		}}
		ch <- llm.Event{Type: llm.EventDone, Usage: usage}
	}
	close(ch)
	return ch, nil
}

func benchRun(b *testing.B, iters int, withSessions, withCosts bool, resultBytes int) {
	b.Setenv("BEE_HOME", b.TempDir())
	reg := tools.NewRegistry()
	body := strings.Repeat("x", resultBytes)
	_ = reg.Register(&stubTool{name: "read", fn: func(context.Context, map[string]any) (tools.Result, error) {
		return tools.Result{Content: body}, nil
	}})
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		eng, _ := newEngine(&instantProvider{n: int32(iters)}, reg)
		if withSessions {
			r, err := session.Open("bench")
			if err != nil {
				b.Fatal(err)
			}
			eng.Sessions = r
			defer r.Close()
		}
		if withCosts {
			eng.Costs = cost.New()
		}
		if _, err := eng.Run(context.Background(), "go"); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*iters)/1000, "us/iter")
}

func BenchmarkOverhead_Bare20(b *testing.B)     { benchRun(b, 20, false, false, 2000) }
func BenchmarkOverhead_Bare90(b *testing.B)     { benchRun(b, 90, false, false, 2000) }
func BenchmarkOverhead_Bare90Big(b *testing.B)  { benchRun(b, 90, false, false, 60000) }
func BenchmarkOverhead_Sessions90(b *testing.B) { benchRun(b, 90, true, false, 2000) }
func BenchmarkOverhead_Costs90(b *testing.B)    { benchRun(b, 90, false, true, 2000) }
func BenchmarkOverhead_Full90(b *testing.B)     { benchRun(b, 90, true, true, 2000) }
