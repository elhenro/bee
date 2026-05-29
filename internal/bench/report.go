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

// WriteTable prints a human scoreboard.
func WriteTable(res SuiteResult, w io.Writer) {
	fmt.Fprintf(w, "\nbench %q — aggregate %.1f  (success %.2f / format %.2f / eff %.2f)\n",
		res.Label, res.Aggregate, res.DimMeans.Success, res.DimMeans.Format, res.DimMeans.Efficiency)
	fmt.Fprintf(w, "%-24s %6s  %4s %4s %4s  %-2s  %s\n", "task", "score", "suc", "fmt", "eff", "ok", "notes")
	for _, t := range res.Tasks {
		ok := "✗"
		if t.Succeeded {
			ok = "✓"
		}
		note := t.Reason
		if t.Err != "" {
			note = "ERR: " + t.Err
		}
		fmt.Fprintf(w, "%-24s %6.1f  %.2f %.2f %.2f  %-2s  %s\n",
			t.ID, t.Score, t.Dims.Success, t.Dims.Format, t.Dims.Efficiency, ok, truncate(note, 60))
	}
	fmt.Fprintln(w)
}
