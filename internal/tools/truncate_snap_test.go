package tools

import (
	"strings"
	"testing"
)

// one early newline in a huge single-line blob must not shrink the kept head
// to a few chars.
func TestTruncate_HeadSnapKeepsMostOfBudget(t *testing.T) {
	blob := "hdr\n" + strings.Repeat("x", 200_000)
	out, truncated := TruncateWithLimitSpill("read", blob, 1000, "")
	if !truncated {
		t.Fatal("expected truncation")
	}
	if len(out) < 3000 {
		t.Errorf("head snapped back to the first newline: kept %d bytes", len(out))
	}

	lines := strings.Repeat(strings.Repeat("y", 49)+"\n", 400)
	out, _ = TruncateWithLimitSpill("read", lines, 1000, "")
	head := out[:strings.Index(out, "\n...\n")]
	if !strings.HasSuffix(head, "y") || len(head) < 3000 {
		t.Errorf("multi-line head should still end on a line boundary, got %d bytes", len(head))
	}
}

func TestTruncate_HeadTailSnapKeepsTail(t *testing.T) {
	blob := strings.Repeat("a", 100_000) + "\n" + strings.Repeat("b", 100_000)
	out, _ := TruncateWithLimitSpill("bash", blob, 1000, "")
	if strings.Count(out, "b") < 500 {
		t.Errorf("tail was snapped away: %d b's kept", strings.Count(out, "b"))
	}
}
