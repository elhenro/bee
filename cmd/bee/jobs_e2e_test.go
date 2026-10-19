package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// the model starts a background job and ends its turn. headless must not
// exit: it waits for the job, then hands the result to the model as a new turn.
func TestScriptedHeadlessBackgroundJobRetriggers(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e under -short")
	}
	bin := buildBee(t)
	home, sessDir := tmpHomeSessions(t)

	fixture := `{"scenarios": [
	  {"name":"start","match":{"contains":"bgtask"},"events":[
	    {"type":"text_delta","delta":"Starting job."},
	    {"type":"tool_use","tool":{"id":"toolu_bg1","name":"bash","input":{"command":"sleep 0.4; echo JOBDONE-MARKER","background":true}}},
	    {"type":"done","stop_reason":"tool_use"}]},
	  {"name":"end-turn","match":{"role":"tool"},"events":[
	    {"type":"text_delta","delta":"Job runs, nothing left for me."},
	    {"type":"done","stop_reason":"stop"}]},
	  {"name":"after-job","match":{"contains":"[job 1 done, exit 0"},"events":[
	    {"type":"text_delta","delta":"Saw the build finish."},
	    {"type":"done","stop_reason":"stop"}]}
	]}`
	script := filepath.Join(t.TempDir(), "bg_job.json")
	if err := os.WriteFile(script, []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "run", "--headless", "bgtask")
	cmd.Env = scriptedEnv(home, sessDir, script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bee run failed: %v\noutput:\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{"Starting job.", "Job runs, nothing left for me.", "background job finished", "Saw the build finish."} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
