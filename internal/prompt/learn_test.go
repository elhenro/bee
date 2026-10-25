package prompt

import (
	"strings"
	"testing"
)

func TestLearnRules_InPrompt(t *testing.T) {
	out := Assemble(baseCfg(), nil, "", nil, nil)
	if !strings.Contains(out, "## Reasons") || !strings.Contains(out, "`why`") {
		t.Errorf("missing the Reasons block: %q", out)
	}
}
