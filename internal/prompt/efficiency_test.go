package prompt

import (
	"strings"
	"testing"
)

func TestEfficiencyRules_NormalOnly(t *testing.T) {
	cfg := baseCfg()
	cfg.Profile = "normal"
	out := Assemble(cfg, nil, "", nil, nil)
	if !strings.Contains(out, "## Efficiency") || !strings.Contains(out, "parallel tool calls") {
		t.Errorf("normal profile missing Efficiency section: %q", out)
	}
	cfg.Profile = "tiny"
	if out := Assemble(cfg, nil, "", nil, nil); strings.Contains(out, "## Efficiency") {
		t.Errorf("tiny profile must keep its own discipline block only")
	}
}
