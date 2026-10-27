package write

// New builds the tool without a path filter, for tests.
func New(root string) *Tool { return NewWithFilter(root, nil) }
