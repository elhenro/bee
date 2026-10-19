package shell

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/jobs"
)

func TestBackgroundFlagOnlyAdvertisedWithManager(t *testing.T) {
	props := func(tl *Tool) map[string]any {
		return tl.Spec().Schema["properties"].(map[string]any)
	}
	if _, ok := props(&Tool{})["background"]; ok {
		t.Fatal("background advertised without a job manager")
	}
	m := jobs.New()
	defer m.Close()
	if _, ok := props(&Tool{opts: Options{Jobs: m}})["background"]; !ok {
		t.Fatal("background missing with a job manager")
	}
}

func TestBackgroundWithoutManagerIsRefused(t *testing.T) {
	res, err := New().Run(context.Background(), map[string]any{"command": "echo hi", "background": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("want refusal, got %q", res.Content)
	}
}

func TestBackgroundReturnsAtOnceAndJobFinishesLater(t *testing.T) {
	m := jobs.New()
	defer m.Close()
	tool := NewWithOptions(nil, Options{Jobs: m})

	start := time.Now()
	res, err := tool.Run(context.Background(), map[string]any{
		"command": "sleep 0.3; echo built", "background": true,
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatalf("background call blocked for %s", time.Since(start))
	}
	if !strings.Contains(res.Content, "started job 1") {
		t.Fatalf("result = %q", res.Content)
	}
	if m.Running() != 1 {
		t.Fatalf("running = %d", m.Running())
	}
	select {
	case <-m.Wake():
	case <-time.After(5 * time.Second):
		t.Fatal("job never finished")
	}
	done := m.TakeDone()
	if len(done) != 1 || !strings.Contains(done[0].Output, "built") {
		t.Fatalf("done = %+v", done)
	}
}

func TestBackgroundSurvivesCallerCtxCancel(t *testing.T) {
	m := jobs.New()
	defer m.Close()
	tool := NewWithOptions(nil, Options{Jobs: m})

	ctx, cancel := context.WithCancel(context.Background())
	if res, _ := tool.Run(ctx, map[string]any{"command": "sleep 30", "background": true}); res.IsError {
		t.Fatalf("start failed: %s", res.Content)
	}
	cancel() // turn ends / esc
	time.Sleep(100 * time.Millisecond)
	if m.Running() != 1 {
		t.Fatal("job died with the turn context")
	}
}

func TestBackgroundStillGoesThroughApproval(t *testing.T) {
	m := jobs.New()
	defer m.Close()
	tool := NewWithOptions(approval.Static{Verdict: approval.Deny}, Options{Jobs: m})
	res, err := tool.Run(context.Background(), map[string]any{
		"command": "rm -rf ./tmpx", "background": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Content, "refused by user") {
		t.Fatalf("res = %+v", res)
	}
	if len(m.List()) != 0 {
		t.Fatal("denied command was spawned")
	}
}

func TestBackgroundStringTrueAccepted(t *testing.T) {
	if !wantsBackground(map[string]any{"background": "true"}) {
		t.Fatal(`"true" should count`)
	}
	if wantsBackground(map[string]any{"background": "no"}) || wantsBackground(map[string]any{}) {
		t.Fatal("non-true must not background")
	}
}
