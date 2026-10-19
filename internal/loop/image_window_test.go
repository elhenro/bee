package loop

import (
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func imageHistory(n int) []types.Message {
	msgs := make([]types.Message, n)
	for i := range msgs {
		msgs[i] = types.Message{Role: types.RoleTool, Content: []types.ContentBlock{
			{Type: types.BlockText, Text: "image · a.png · attached below"},
			{Type: types.BlockImage, MediaType: "image/png", Data: []byte{byte(i)}},
		}}
	}
	return msgs
}

func countImages(msgs []types.Message) (n int, first int) {
	first = -1
	for i, m := range msgs {
		for _, b := range m.Content {
			if b.Type == types.BlockImage {
				if first < 0 {
					first = i
				}
				n++
			}
		}
	}
	return n, first
}

func TestCapRecentImages(t *testing.T) {
	cases := []struct{ total, kept int }{
		{0, 0}, {1, 1}, {imageWindow, imageWindow},
		{2*imageWindow - 1, 2*imageWindow - 1},
		{2 * imageWindow, imageWindow},
		{2*imageWindow + 3, imageWindow + 3},
		{40, 8},
	}
	for _, c := range cases {
		src := imageHistory(c.total)
		out := capRecentImages(src)
		got, first := countImages(out)
		if got != c.kept {
			t.Errorf("total %d: kept %d images, want %d", c.total, got, c.kept)
		}
		// newest are the survivors
		if got > 0 && first != c.total-got {
			t.Errorf("total %d: first kept image at msg %d, want %d", c.total, first, c.total-got)
		}
		if n, _ := countImages(src); n != c.total {
			t.Errorf("total %d: source mutated, %d images left", c.total, n)
		}
	}
}

// the prefix only changes when a whole step is evicted, so between evictions
// the oldest surviving image stays the same.
func TestCapRecentImagesStablePrefix(t *testing.T) {
	_, a := countImages(capRecentImages(imageHistory(imageWindow + imageStep)))
	_, b := countImages(capRecentImages(imageHistory(imageWindow + imageStep + 3)))
	if a != imageStep || b != imageStep {
		t.Errorf("eviction point moved within a step: %d vs %d", a, b)
	}
}

func TestCapRecentImagesKeepsNote(t *testing.T) {
	out := capRecentImages(imageHistory(3 * imageWindow))
	if out[0].Content[0].Text == "" || out[0].Content[1].Text != imageDroppedNote {
		t.Errorf("dropped image should leave tool text + note, got %+v", out[0].Content)
	}
}
