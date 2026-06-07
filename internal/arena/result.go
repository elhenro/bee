package arena

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
)

// MatchResult is one row in the append-only wars ledger: enough to rank and
// audit a match without replaying its transcripts. Both session ids are kept so
// a full replay is one `bee back <id>` away. The secret is stored only as a
// hash — plaintext never lands in the ledger.
type MatchResult struct {
	Time        string `json:"time"`
	MatchID     string `json:"match_id"`
	Winner      string `json:"winner"` // red | blue | draw
	Reason      string `json:"reason"` // exfiltration | opponent_bankrupt | self_bankrupt | round_cap
	Rounds      int    `json:"rounds"`
	RedModel    string `json:"red_model,omitempty"`
	BlueModel   string `json:"blue_model,omitempty"`
	StartTokens int    `json:"start_tokens,omitempty"`
	RedBalance  int    `json:"red_balance"`
	BlueBalance int    `json:"blue_balance"`
	RedSpent    int    `json:"red_spent"`
	BlueSpent   int    `json:"blue_spent"`
	Messages    int    `json:"messages"`
	RedSession  string `json:"red_session,omitempty"`
	BlueSession string `json:"blue_session,omitempty"`
	SecretHash  string `json:"secret_hash,omitempty"`
	Economy     string `json:"economy,omitempty"`
	Config      string `json:"config,omitempty"`
}

// AppendResult appends one JSON line to the wars ledger at path, creating parent
// dirs as needed. Empty path disables logging. A ledger write must never fail
// the match, so callers report the error and continue. Mirrors
// bench.AppendLedger.
func AppendResult(path string, r MatchResult) error {
	if path == "" {
		return nil
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	line, err := json.Marshal(r)
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

// Elo returns updated ratings after a match. scoreA is from a's perspective
// (1 win, 0.5 draw, 0 loss); k is the rating volatility (24 is stable for noisy
// adversarial games). The exchange is zero-sum: b's delta is the negative of
// a's, so total rating is conserved.
func Elo(a, b, scoreA, k float64) (newA, newB float64) {
	expectedA := 1.0 / (1.0 + math.Pow(10, (b-a)/400.0))
	deltaA := k * (scoreA - expectedA)
	return a + deltaA, b - deltaA
}

// EloScore maps a match winner to the score for one side, from that side's
// perspective: 1 win, 0 loss, 0.5 draw.
func EloScore(winner, side string) float64 {
	switch {
	case winner == "draw" || winner == "":
		return 0.5
	case winner == side:
		return 1.0
	default:
		return 0.0
	}
}
