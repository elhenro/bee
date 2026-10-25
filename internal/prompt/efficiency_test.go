package prompt

import (
	"strings"
	"testing"
)

func TestEfficiencyRules_InPrompt(t *testing.T) {
	out := Assemble(baseCfg(), nil, "", nil, nil)
	if !strings.Contains(out, "## Efficiency") || !strings.Contains(out, "parallel tool calls") {
		t.Errorf("missing Efficiency section: %q", out)
	}
}
