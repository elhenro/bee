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

func TestLookupDottedRoutedIds(t *testing.T) {
	for _, m := range []string{"anthropic/claude-sonnet-4.5", "claude-sonnet-4.5", "moonshotai/kimi-k2.5"} {
		if _, ok := Lookup("openrouter", m); !ok {
			t.Errorf("Lookup(%q) missed", m)
		}
	}
}

func TestRecordReportedPrefersProviderCost(t *testing.T) {
	tr := New()
	ev := tr.RecordReported("openrouter", "vendor/unpriced-model", 1000, 100, 0.0123)
	if ev.USD != 0.0123 {
		t.Errorf("USD = %v, want reported 0.0123", ev.USD)
	}
	if got := tr.Total().USD; got != 0.0123 {
		t.Errorf("Total USD = %v", got)
	}
	est := tr.RecordReported("openai", "gpt-4o", 1_000_000, 0, 0)
	if est.USD != 2.50 {
		t.Errorf("zero reported must fall back to table estimate, got %v", est.USD)
	}
}

// cache reads bill at the cached share of the input rate; in includes them.
func TestRecordCachedDiscountsCacheReads(t *testing.T) {
	tr := New()
	// claude-sonnet-4-5 input 3.00/M: 100k fresh + 900k cached at 0.1x, 10k out at 15/M
	ev := tr.RecordCached("anthropic", "claude-sonnet-4-5", 1_000_000, 900_000, 10_000, 0)
	want := 0.1*3.00 + 0.9*3.00*0.1 + 0.01*15.00
	if math.Abs(ev.USD-want) > 1e-9 {
		t.Fatalf("usd: got %f want %f", ev.USD, want)
	}
	if ev.Cached != 900_000 || tr.Total().Input != 1_000_000 {
		t.Fatalf("event %+v total %+v", ev, tr.Total())
	}
}

// a family without a documented discount pays the full rate on cached tokens.
func TestRecordCachedUnknownFamilyPaysFullRate(t *testing.T) {
	tr := New()
	a := tr.RecordCached("deepseek", "deepseek-chat", 1_000_000, 800_000, 0, 0)
	b := tr.Record("deepseek", "deepseek-chat", 1_000_000, 0)
	if a.USD != b.USD {
		t.Fatalf("cached %f != uncached %f for a model with no known discount", a.USD, b.USD)
	}
}

// bogus cached counts are clamped into [0, in].
func TestRecordCachedClamps(t *testing.T) {
	tr := New()
	over := tr.RecordCached("anthropic", "claude-sonnet-4-5", 1000, 5000, 0, 0)
	all := tr.RecordCached("anthropic", "claude-sonnet-4-5", 1000, 1000, 0, 0)
	neg := tr.RecordCached("anthropic", "claude-sonnet-4-5", 1000, -5, 0, 0)
	full := tr.Record("anthropic", "claude-sonnet-4-5", 1000, 0)
	if over.USD != all.USD || neg.USD != full.USD {
		t.Fatalf("over %f all %f neg %f full %f", over.USD, all.USD, neg.USD, full.USD)
	}
}

func TestLookupStripsSnapshotAndLatest(t *testing.T) {
	want, _ := Lookup("anthropic", "claude-sonnet-4-5")
	for _, id := range []string{
		"claude-sonnet-4-5-20250929",
		"claude-sonnet-4-5-latest",
		"anthropic/claude-sonnet-4.5",
		"anthropic/claude-sonnet-4.5-20250929",
	} {
		got, ok := Lookup("anthropic", id)
		if !ok || got != want {
			t.Errorf("Lookup(%q) = %+v, %v; want %+v", id, got, ok, want)
		}
	}
	if want, _ := Lookup("openai", "gpt-4o-mini"); true {
		if got, ok := Lookup("openai", "gpt-4o-mini-2024-07-18"); !ok || got != want {
			t.Errorf("dated openai id = %+v, %v; want %+v", got, ok, want)
		}
	}
	// a suffix strip must not invent a price for an unlisted model
	if _, ok := Lookup("anthropic", "claude-unlisted-9-20250929"); ok {
		t.Error("unlisted dated model priced")
	}
}

// the running rollup agrees with the event log through records and a reset.
func TestTotalIsIncremental(t *testing.T) {
	tr := New()
	tr.Record("openai", "gpt-4o-mini", 1000, 500)
	tr.RecordReported("openrouter", "vendor/unpriced", 200, 100, 0.5)
	var sum Summary
	for _, e := range tr.Events() {
		sum.add(e.Input, e.Output, e.USD)
	}
	if got := tr.Total(); got != sum {
		t.Fatalf("Total %+v != event sum %+v", got, sum)
	}
	tr.Reset()
	if got := tr.Total(); got != (Summary{}) {
		t.Fatalf("Total after Reset = %+v", got)
	}
}
