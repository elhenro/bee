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
// (max−min across runs) so a reader can see how noisy each score is.
func WriteTable(res SuiteResult, w io.Writer) {
	runs := res.Runs
	if runs < 1 {
		runs = 1
	}
	spreadNote := ""
	if runs > 1 {
		spreadNote = fmt.Sprintf(" ±%.1f over %d runs", res.MeanSpread, runs)
	}
	fmt.Fprintf(w, "\nbench %q — aggregate %.1f%s  (success %.2f / format %.2f / eff %.2f)\n",
		res.Label, res.Aggregate, spreadNote, res.DimMeans.Success, res.DimMeans.Format, res.DimMeans.Efficiency)
	fmt.Fprintf(w, "%-24s %6s %5s  %4s %4s %4s  %-2s  %s\n", "task", "score", "±", "suc", "fmt", "eff", "ok", "notes")
	for _, t := range res.Tasks {
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
