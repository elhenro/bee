package hashline_edit

import "github.com/elhenro/bee/internal/tools"

// New builds the tool without a path filter, for tests.
func New(root string) tools.Tool { return NewWithFilter(root, nil) }
