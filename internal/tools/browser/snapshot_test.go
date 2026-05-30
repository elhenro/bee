package browser

import (
	"strings"
	"testing"
)

func TestSnapshotJS_WellFormed(t *testing.T) {
	if snapshotJS == "" {
		t.Fatal("snapshotJS empty")
	}
	for _, must := range []string{"data-bee-ref", "button", "return", "function"} {
		if !strings.Contains(snapshotJS, must) {
			t.Errorf("snapshotJS missing %q", must)
		}
	}
}

func TestRefAttr(t *testing.T) {
	if refAttr != "data-bee-ref" {
		t.Errorf("refAttr = %q", refAttr)
	}
	if got := refSelector("e5"); got != "[data-bee-ref='e5']" {
		t.Errorf("refSelector = %q", got)
	}
}
