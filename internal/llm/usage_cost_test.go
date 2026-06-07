package llm

import (
	"encoding/json"
	"testing"

	"github.com/elhenro/bee/internal/llm/wire"
)

func TestReportsCostGatesUsageFlag(t *testing.T) {
	on := NewOpenAICompat(OpenAICompatConfig{Name: "agg", ReportsCost: true})
	wr := on.buildWireRequest(Request{Model: "m", Stream: true})
	if wr.Usage == nil || !wr.Usage.Include {
		t.Fatalf("ReportsCost=true should set usage.include; got %+v", wr.Usage)
	}

	off := NewOpenAICompat(OpenAICompatConfig{Name: "strict"})
	wr2 := off.buildWireRequest(Request{Model: "m", Stream: true})
	if wr2.Usage != nil {
		t.Fatalf("ReportsCost=false should omit usage field; got %+v", wr2.Usage)
	}
	// the field must marshal away entirely so strict endpoints never see it.
	b, _ := json.Marshal(wr2)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, present := m["usage"]; present {
		t.Error("strict request body should not contain a usage field")
	}
}

func TestUsageFromWireCapturesCostAndCached(t *testing.T) {
	var su wire.StreamUsage
	if err := json.Unmarshal([]byte(`{"prompt_tokens":10,"completion_tokens":5,"cost":0.0031,"prompt_tokens_details":{"cached_tokens":4}}`), &su); err != nil {
		t.Fatal(err)
	}
	u := usageFromWire(&su)
	if u.InputTokens != 10 || u.OutputTokens != 5 {
		t.Errorf("tokens = %d/%d, want 10/5", u.InputTokens, u.OutputTokens)
	}
	if u.CostUSD != 0.0031 {
		t.Errorf("cost = %v, want 0.0031", u.CostUSD)
	}
	if u.CachedTokens != 4 {
		t.Errorf("cached = %d, want 4", u.CachedTokens)
	}
}
