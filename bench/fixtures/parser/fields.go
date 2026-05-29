package parser

// SplitFields splits a comma-separated record into trimmed, non-empty fields.
// rules: trim surrounding spaces from each field; drop empty fields (so
// trailing commas and runs of commas never produce blanks); an empty or
// whitespace-only input yields an empty, non-nil slice.
func SplitFields(s string) []string {
	// TODO: implement per the rules above
	return nil
}
