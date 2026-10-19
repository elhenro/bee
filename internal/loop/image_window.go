package loop

import "github.com/elhenro/bee/internal/types"

const (
	// imageWindow is the floor of pictures a request carries; imageStep is how
	// many go at once. Each read of a picture adds ~100-400KB of base64 to every
	// later request, so an unbounded history turns a review loop into 20MB
	// uploads, 30s iterations and a model that garbles its tool args. Evicting in
	// steps keeps the request prefix stable between evictions (prompt cache).
	imageWindow = 8
	imageStep   = 8

	imageDroppedNote = "[older image dropped from context to keep requests small; its tool result above names the file, re-read it only if you must see it again]"
)

// capRecentImages returns a wire copy of msgs with the oldest image blocks
// swapped for a text note once more than 2*imageWindow-1 are present, leaving
// between imageWindow and 2*imageWindow-1 of the newest. The session's own
// messages keep every image.
func capRecentImages(msgs []types.Message) []types.Message {
	total := 0
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Type == types.BlockImage {
				total++
			}
		}
	}
	drop := (total - imageWindow) / imageStep * imageStep
	if drop <= 0 {
		return msgs
	}
	out := make([]types.Message, len(msgs))
	for i, m := range msgs {
		out[i] = m
		if drop == 0 || !msgHasImage(m) {
			continue
		}
		nc := make([]types.ContentBlock, 0, len(m.Content))
		for _, b := range m.Content {
			if b.Type == types.BlockImage && drop > 0 {
				drop--
				b = types.ContentBlock{Type: types.BlockText, Text: imageDroppedNote}
			}
			nc = append(nc, b)
		}
		out[i].Content = nc
	}
	return out
}
