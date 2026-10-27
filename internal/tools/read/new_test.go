package read

import "github.com/elhenro/bee/internal/tools"

// New builds the tool with package-default limits and secret-path blocking, for tests.
func New() tools.Tool { return NewWithLimits(0, 0, true) }
