package paginator

// Page returns the 1-based page slice of items for the given page size.
// page 1 is the first page. an out-of-range page returns an empty slice.
func Page(items []int, page, size int) []int {
	if size <= 0 || page <= 0 {
		return nil
	}
	start := page * size
	end := start + size
	if start >= len(items) {
		return nil
	}
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}
