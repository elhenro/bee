package loop

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"context"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

// a hosted model missing from the table must not hold the first request behind
// a /models round trip.
func TestRunDoesNotWaitOnHostedContextProbe(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	p := &stubProvider{scripts: [][]llm.Event{{
		{Type: llm.EventTextDelta, Delta: "ok"},
		{Type: llm.EventDone, StopReason: "stop"},
	}}}
	eng, _ := newEngine(p, tools.NewRegistry())
	eng.Cfg.DefaultProvider = "slowprobe"
	eng.Cfg.DefaultModel = "mystery-model-for-probe-test"
	eng.Cfg.Providers = map[string]config.ProviderConfig{
		"slowprobe": {BaseURL: srv.URL, WireAPI: "chat"},
	}
	start := time.Now()
	if _, err := eng.Run(context.Background(), "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Run took %v with a hanging /models endpoint, probe is back on the critical path", d)
	}
}
