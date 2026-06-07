package arena

import (
	"fmt"

	"github.com/elhenro/bee/internal/knowledge"
)

// WritePostMortem documents a finished match into a knowledge store so agents
// can research prior bouts before the next one. v1 records the factual outcome
// (winner, path, economy state); richer model-extracted lessons are a later
// enhancement layered on the same store.
func WritePostMortem(dir string, r MatchResult) (string, error) {
	name := "beewars-" + r.MatchID
	desc := fmt.Sprintf("bee-wars match %s: %s won by %s", r.MatchID, r.Winner, r.Reason)
	body := fmt.Sprintf(`# bee-wars post-mortem %s

- winner: %s
- reason: %s
- rounds: %d
- red model: %s (final nectar %d, spent %d)
- blue model: %s (final nectar %d, spent %d)
- economy: %s

## takeaway

The %s side prevailed via %q. Review the linked session transcripts to extract
the winning attack route and the defense that failed.
`,
		r.MatchID, r.Winner, r.Reason, r.Rounds,
		r.RedModel, r.RedBalance, r.RedSpent,
		r.BlueModel, r.BlueBalance, r.BlueSpent,
		r.Economy, r.Winner, r.Reason)

	rec := knowledge.Record{
		Entry: knowledge.Entry{
			Name:        name,
			Description: desc,
			Tags:        []string{"beewars", "match"},
			Priority:    3,
		},
		Body: body,
	}
	return knowledge.WriteRecord(dir, rec)
}
