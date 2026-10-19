package session

import (
	"encoding/json"
	"fmt"

	"github.com/elhenro/bee/internal/types"
)

// marshalLine renders msg as one JSONL line. Readers skip a line over
// maxLineBytes, which would silently lose the message and orphan its
// tool_use/tool_result partner, so an oversized message is persisted with its
// images replaced by a placeholder, or refused when text alone is too big.
func marshalLine(msg types.Message) ([]byte, error) {
	b, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	if len(b)+1 > maxLineBytes {
		if b, err = json.Marshal(withoutImages(msg)); err != nil {
			return nil, err
		}
		if len(b)+1 > maxLineBytes {
			return nil, fmt.Errorf("session: message %s is %d bytes, over the %d byte line limit", msg.ID, len(b)+1, maxLineBytes)
		}
	}
	return append(b, '\n'), nil
}

func withoutImages(m types.Message) types.Message {
	out := m
	out.Content = make([]types.ContentBlock, len(m.Content))
	for i, c := range m.Content {
		if c.Type == types.BlockImage {
			c = types.ContentBlock{Type: types.BlockText, Text: "[image omitted: too large to persist]"}
		}
		out.Content[i] = c
	}
	return out
}
