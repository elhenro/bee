package prompt

import (
	"strings"
	"testing"
)

func TestLearnRules_SkippedOnTiny(t *testing.T) {
	cfg := baseCfg()
	cfg.Profile = "normal"
	if out := Assemble(cfg, nil, "", nil, nil); !strings.Contains(out, "## Reasons") || !strings.Contains(out, "`why`") {
		t.Errorf("normal profile missing the Reasons block: %q", out)
	}
	cfg.Profile = "tiny"
	if out := Assemble(cfg, nil, "", nil, nil); strings.Contains(out, "## Reasons") {
		t.Errorf("tiny profile must not pay for the Reasons block")
	}
}
