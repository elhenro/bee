package cost

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// queued usage lines and lifetime deltas are on disk once Flush returns.
func TestFlushLandsQueuedWrites(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(Flush) // after TempDir's cleanup is registered, so it runs first
	t.Setenv("BEE_USAGE_LOG", filepath.Join(dir, "usage.jsonl"))
	t.Setenv("BEE_LIFETIME_TOKENS", filepath.Join(dir, "life.json"))
	ResetUsageForTest()
	ResetLifetimeForTest()

	for range 20 {
		AppendUsage(UsageRecord{Time: time.Now(), Provider: "p", Model: "m", Input: 10, Output: 5})
		AddLifetime(10, 5)
	}
	Flush()

	if got := readLifetime(filepath.Join(dir, "life.json")); got.Input != 200 || got.Output != 100 {
		t.Errorf("lifetime file = %+v, want 200/100", got)
	}
	recs, err := ReadUsage()
	if err != nil || len(recs) != 20 {
		t.Fatalf("usage lines = %d, err %v, want 20", len(recs), err)
	}
}

// concurrent producers lose nothing, and totals include what is still queued.
func TestWriterConcurrentProducers(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(Flush)
	t.Setenv("BEE_USAGE_LOG", filepath.Join(dir, "usage.jsonl"))
	t.Setenv("BEE_LIFETIME_TOKENS", filepath.Join(dir, "life.json"))
	ResetUsageForTest()
	ResetLifetimeForTest()

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				AppendUsage(UsageRecord{Provider: "p", Model: "m", Input: 1, Output: 1})
				AddLifetime(2, 1)
			}
		}()
	}
	wg.Wait()

	in, out := LifetimeTotals()
	if in != 400 || out != 200 {
		t.Errorf("totals = (%d,%d), want (400,200)", in, out)
	}
	if recs, _ := ReadUsage(); len(recs) != 200 {
		t.Errorf("usage lines = %d, want 200", len(recs))
	}
}

// the second bee process's delta merges into the file rather than replacing it.
func TestLifetimeMergesWithOtherProcess(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(Flush)
	path := filepath.Join(dir, "life.json")
	t.Setenv("BEE_LIFETIME_TOKENS", path)
	ResetLifetimeForTest()

	AddLifetime(100, 10)
	Flush()
	// another process bumps the file behind our cache
	if _, ok := mergeLifetime(path, lifetimeFile{Input: 50, Output: 5}); !ok {
		t.Fatal("merge failed")
	}
	AddLifetime(1, 1)
	in, out := LifetimeTotals()
	if in != 151 || out != 16 {
		t.Errorf("totals = (%d,%d), want (151,16)", in, out)
	}
}
