package bench

import "github.com/elhenro/bee/internal/jsonl"

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
	WallMeanMillis   int64   `json:"wall_mean_millis,omitempty"`
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
	return jsonl.Append(path, rec)
}
