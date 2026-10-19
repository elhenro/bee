package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/elhenro/bee/internal/types"
)

func TestCutBytes_NeverSplitsARune(t *testing.T) {
	s := "x" + strings.Repeat("ä", 10)
	for n := 0; n <= len(s)+2; n++ {
		got := cutBytes(s, n)
		if !utf8.ValidString(got) || len(got) > n && n < len(s) || !strings.HasPrefix(s, got) {
			t.Fatalf("cutBytes(%d) = %q", n, got)
		}
	}
}

func TestTruncation_StaysValidUTF8(t *testing.T) {
	n := &types.MessageNode{Msg: types.Message{ID: "abcdefghijkl", Role: types.RoleUser,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: "x" + strings.Repeat("ä", 30)}}}}
	if l := nodeLabel(n); !utf8.ValidString(l) {
		t.Errorf("nodeLabel = %q", l)
	}
	if l := truncateOneLine(strings.Repeat("ä", 70), 60); !utf8.ValidString(l) {
		t.Errorf("truncateOneLine = %q", l)
	}
	// two names sharing only the first byte of their second rune
	if p := LongestCommonPrefix([]string{"Süd.md", "Sön.md"}); p != "S" {
		t.Errorf("LongestCommonPrefix = %q, want %q", p, "S")
	}
}
