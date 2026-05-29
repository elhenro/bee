// `bee bench`: run a task suite through the headless /goal loop against the
// configured (local) model and emit a scored scoreboard. Measurement only —
// tuning bee from the scores is the caller's job.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/bench"
)

func runBench(args []string) {
	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	suite := fs.String("suite", "bench/tasks", "directory of *.json task specs")
	holdout := fs.String("holdout", "bench/holdout", "directory of never-tuned *.json tasks scored separately (skipped if absent/empty)")
	out := fs.String("out", "bench/results", "directory for the results JSON")
	label := fs.String("label", "run", "tag for this result set (config-variant id)")
	provider := fs.String("provider", "", "override default_provider for the runs")
	model := fs.String("model", "", "override the model for the runs")
	configFile := fs.String("config", "", "BEE_CONFIG overlay path (sweep knobs without touching ~/.bee/config.toml)")
	profile := fs.String("profile", "", "BEE_PROFILE override (tiny|normal|large|<custom>)")
	weightsCSV := fs.String("weights", "", "success,format,efficiency (e.g. 0.6,0.25,0.15)")
	timeout := fs.Duration("timeout", 5*time.Minute, "per-task wall-clock cap")
	runs := fs.Int("runs", 1, "repeats per task for variance (reports mean ± spread)")
	rollouts := fs.String("rollouts", "", "persist blessed (passed + clean) session jsonl here for fine-tune harvest")
	jsonOnly := fs.Bool("json", false, "print only the results JSON path")
	_ = fs.Parse(args)

	tasks, err := bench.LoadTasks(*suite)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bench:", err)
		os.Exit(1)
	}
	if len(tasks) == 0 {
		fmt.Fprintln(os.Stderr, "bench: no tasks in", *suite)
		os.Exit(1)
	}

	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "bench: locate self:", err)
		os.Exit(1)
	}

	opt := bench.Options{
		BeeBin:     self,
		Provider:   *provider,
		Model:      *model,
		ConfigFile: *configFile,
		Profile:    *profile,
		Label:      *label,
		Timeout:    *timeout,
		Runs:       *runs,
		RolloutDir: *rollouts,
	}
	if w, ok := parseWeights(*weightsCSV); ok {
		opt.Weights = w
	}

	ctx := context.Background()
	res, err := bench.RunSuite(ctx, tasks, opt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bench:", err)
		os.Exit(1)
	}

	// held-out slice runs after the main suite and is reported separately so a
	// tuning loop never optimizes against it.
	if err := bench.AttachHoldout(ctx, &res, *holdout, opt); err != nil {
		fmt.Fprintln(os.Stderr, "bench: holdout:", err)
		os.Exit(1)
	}

	path, err := bench.WriteJSON(res, *out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bench: write results:", err)
		os.Exit(1)
	}
	if *jsonOnly {
		fmt.Println(path)
		return
	}
	bench.WriteTable(res, os.Stdout)
	fmt.Println("results:", path)
}

func parseWeights(csv string) (bench.Weights, bool) {
	if strings.TrimSpace(csv) == "" {
		return bench.Weights{}, false
	}
	parts := strings.Split(csv, ",")
	if len(parts) != 3 {
		return bench.Weights{}, false
	}
	var f [3]float64
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return bench.Weights{}, false
		}
		f[i] = v
	}
	return bench.Weights{Success: f[0], Format: f[1], Efficiency: f[2]}, true
}
