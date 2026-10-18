package loop

import (
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func TestToolImageBlocks(t *testing.T) {
	img := func(s string) types.ContentBlock {
		return types.ContentBlock{Type: types.BlockImage, MediaType: "image/png", Data: []byte(s)}
	}
	got := toolImageBlocks([]types.ToolResult{
		{UseID: "a", Images: []types.ContentBlock{img("1")}},
		{UseID: "b"},
		{UseID: "c", IsError: true, Images: []types.ContentBlock{img("dropped")}},
		{UseID: "d", Images: []types.ContentBlock{img("2"), img("3")}},
	})
	var data []string
	for _, b := range got {
		data = append(data, string(b.Data))
	}
	if len(data) != 3 || data[0] != "1" || data[1] != "2" || data[2] != "3" {
		t.Fatalf("want call order 1,2,3 without the error result's image, got %v", data)
	}
}
