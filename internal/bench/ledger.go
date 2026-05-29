package bench

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// LedgerRecord is one row in the append-only benchmark ledger: enough to rank
// and audit a run without reopening its results JSON.
type LedgerRecord struct {
	Time             string  `json:"time"`
	Label            string  `json:"label"`
	Provider         string  `json:"provider,omitempty"`
	Model            string  `json:"model,omitempty"`
	Profile          string  `json:"profile,omitempty"`
	Config           string  `json:"config,omitempty"`
	Suite            string  `json:"suite"`
	Tasks            int     `json:"tasks"`
	Runs             int     `json:"runs"`
	Aggregate        float64 `json:"aggregate"`
	Success          float64 `json:"success"`
	Format           float64 `json:"format"`
	Efficiency       float64 `json:"efficiency"`
	HoldoutAggregate float64 `json:"holdout_aggregate,omitempty"`
	ResultsJSON      string  `json:"results_json"`
}

// AppendLedger appends one JSON line to the ledger at path, creating parent
// dirs as needed. Empty path disables logging. A ledger write must never fail
// the run, so callers report the error and continue.
func AppendLedger(path string, rec LedgerRecord) error {
	if path == "" {
		return nil
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}
