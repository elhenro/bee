package loop

import (
	"time"

	"github.com/elhenro/bee/internal/jsonmode"
)

// harness gauges: the wall time bee itself spends before the next model request,
// emitted as {"type":"harness","name":...,"ms":...} events under --json.
//
//	first    Run start to the first request (knowledge lookup, prompt assembly,
//	         pre-compact, session write)
//	between  stream end to the next request, tool execution excluded (persist,
//	         repeat observers, compaction)
//
// Both should sit near zero; model time and tool time are not harness time.

// gaugeArm starts a gauge. kind is "first" or "between".
func (e *Engine) gaugeArm(kind string) {
	if e.run == nil {
		return
	}
	e.run.gaugeFrom = time.Now()
	e.run.gaugeKind = kind
	e.run.gaugePending = true
	e.run.toolDur = 0
}

// gaugeToolTime subtracts a tool dispatch from the pending "between" gauge.
func (e *Engine) gaugeToolTime(d time.Duration) {
	if e.run != nil {
		e.run.toolDur += d
	}
}

// gaugeRequest closes the pending gauge just before a provider request opens.
// Later attempts of the same iteration (pre-content retries) find nothing pending.
func (e *Engine) gaugeRequest() {
	if e.run == nil || !e.run.gaugePending {
		return
	}
	e.run.gaugePending = false
	if e.JSONEmitter == nil {
		return
	}
	ms := float64(time.Since(e.run.gaugeFrom)-e.run.toolDur) / float64(time.Millisecond)
	if ms < 0 {
		ms = 0
	}
	e.JSONEmitter.Emit(jsonmode.Event{Type: "harness", Name: e.run.gaugeKind, Ms: &ms})
}
