package bench

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WriteJSON persists the scoreboard to dir/<label>.json for the tune skill to
// diff across runs. No timestamp baked into the content — file mtime carries
// time, keeping diffs clean.
func WriteJSON(res SuiteResult, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	label := res.Label
	if label == "" {
		label = "run"
	}
	path := filepath.Join(dir, label+".json")
	raw, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// WriteTable prints a human scoreboard. With repeats it appends the spread
// (max−min across runs) so a reader can see how noisy each score is. When a
// held-out slice ran, it prints below the main suite as a separate section.
func WriteTable(res SuiteResult, w io.Writer) {
	runs := res.Runs
	if runs < 1 {
		runs = 1
	}
	writeSection(w, res.Label, res.Aggregate, res.MeanSpread, res.DimMeans, res.Tasks, runs)
	if len(res.HoldoutTasks) > 0 {
		// held-out spread isn't aggregated separately; the slice exists to gate
		// overfitting, not to report noise, so the per-section mean spread is 0.
		writeSection(w, res.Label+" [held-out]", res.HoldoutAggregate, 0, res.HoldoutDimMeans, res.HoldoutTasks, runs)
	}
}

// writeSection prints one labeled scoreboard block (main or held-out).
func writeSection(w io.Writer, label string, agg, meanSpread float64, dims Dims, tasks []TaskResult, runs int) {
	spreadNote := ""
	if runs > 1 {
		spreadNote = fmt.Sprintf(" ±%.1f over %d runs", meanSpread, runs)
	}
	fmt.Fprintf(w, "\nbench %q — aggregate %.1f%s  (success %.2f / format %.2f / eff %.2f)\n",
		label, agg, spreadNote, dims.Success, dims.Format, dims.Efficiency)
	fmt.Fprintf(w, "%-24s %6s %5s  %4s %4s %4s  %-2s  %s\n", "task", "score", "±", "suc", "fmt", "eff", "ok", "notes")
	for _, t := range tasks {
		ok := "✗"
		if t.Succeeded {
			ok = "✓"
		}
		note := t.Reason
		if t.Err != "" {
			note = "ERR: " + t.Err
		}
		spread := ""
		if runs > 1 {
			spread = fmt.Sprintf("%.1f", t.Spread)
		}
		fmt.Fprintf(w, "%-24s %6.1f %5s  %.2f %.2f %.2f  %-2s  %s\n",
			t.ID, t.Score, spread, t.Dims.Success, t.Dims.Format, t.Dims.Efficiency, ok, truncate(note, 60))
	}
	fmt.Fprintln(w)
}
