package cost

import (
	"math"
	"testing"
)

func TestRecordAndTotal(t *testing.T) {
	tr := New()
	tr.Record("openrouter", "deepseek/deepseek-v4-flash", 1_000_000, 500_000)
	tr.Record("openai", "gpt-4o-mini", 200_000, 100_000)

	tot := tr.Total()
	if tot.Calls != 2 {
		t.Fatalf("calls: %d", tot.Calls)
	}
	if tot.Input != 1_200_000 || tot.Output != 600_000 {
		t.Fatalf("tokens: %+v", tot)
	}
	// deepseek-v4-flash: 1M*0.07 + 0.5M*1.10 = 0.07 + 0.55 = 0.62
	// gpt-4o-mini:       0.2M*0.15 + 0.1M*0.60 = 0.03 + 0.06 = 0.09
	want := 0.62 + 0.09
	if math.Abs(tot.USD-want) > 1e-9 {
		t.Fatalf("usd: got %f want %f", tot.USD, want)
	}
}

func TestResetClearsEvents(t *testing.T) {
	tr := New()
	tr.Record("openai", "gpt-4o-mini", 1234, 56)
	if tr.LastInput() == 0 || tr.Total().Calls == 0 {
		t.Fatal("setup: expected recorded event")
	}
	tr.Reset()
	if tr.LastInput() != 0 {
		t.Errorf("LastInput after Reset: %d, want 0", tr.LastInput())
	}
	if tr.Total().Calls != 0 {
		t.Errorf("Total.Calls after Reset: %d, want 0", tr.Total().Calls)
	}
}

func TestLookupResolvesProviderPrefix(t *testing.T) {
	p, ok := Lookup("openrouter", "openai/gpt-4o-mini")
	if !ok {
		t.Fatal("expected hit via path-suffix")
	}
	if p.InputPerM != 0.15 {
		t.Fatalf("price: %+v", p)
	}
}

func TestLookupMiss(t *testing.T) {
	_, ok := Lookup("ollama", "llama3.1:8b")
	if ok {
		t.Fatal("unexpected hit for unpriced local model")
	}
}

func TestFilterByModel(t *testing.T) {
	tr := New()
	tr.Record("openai", "gpt-4o-mini", 100, 100)
	tr.Record("anthropic", "claude-sonnet-4-5", 100, 100)
	if got := tr.Filter("", "gpt-4o-mini"); len(got) != 1 {
		t.Fatalf("model filter: %d", len(got))
	}
	if got := tr.Filter("openai", ""); len(got) != 1 {
		t.Fatalf("provider filter: %d", len(got))
	}
}

func TestByModelByProvider(t *testing.T) {
	tr := New()
	tr.Record("openai", "gpt-4o-mini", 1000, 500)
	tr.Record("openai", "gpt-4o-mini", 2000, 1000)
	tr.Record("anthropic", "claude-sonnet-4-5", 100, 50)

	bm := tr.ByModel()
	if bm["gpt-4o-mini"].Calls != 2 || bm["gpt-4o-mini"].Input != 3000 {
		t.Fatalf("ByModel: %+v", bm)
	}
	bp := tr.ByProvider()
	if bp["openai"].Calls != 2 || bp["anthropic"].Calls != 1 {
		t.Fatalf("ByProvider: %+v", bp)
	}
}

func TestSetPriceRoundTrip(t *testing.T) {
	prev := SetPrice("zzz-test-model", Price{InputPerM: 1.0, OutputPerM: 2.0})
	defer SetPrice("zzz-test-model", prev)
	p, ok := Lookup("any", "zzz-test-model")
	if !ok || p.InputPerM != 1.0 || p.OutputPerM != 2.0 {
		t.Fatalf("SetPrice failed: %+v ok=%v", p, ok)
	}
}
