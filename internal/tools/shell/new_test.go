package shell

import "github.com/elhenro/bee/internal/tools"

// New builds the tool with no approval gating, for tests.
func New() tools.Tool { return &Tool{} }
