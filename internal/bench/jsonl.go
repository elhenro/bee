package bench

import (
	"bufio"
	"encoding/json"
	"os"

	"github.com/elhenro/bee/internal/types"
)

// parseJSONL reads an append-only session file: one types.Message per line.
func parseJSONL(path string) ([]types.Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var msgs []types.Message
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var m types.Message
		if err := json.Unmarshal(line, &m); err != nil {
			continue // tolerate non-message lines (headers, blanks)
		}
		msgs = append(msgs, m)
	}
	return msgs, sc.Err()
}
