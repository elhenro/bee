package paginator

// PageCount returns how many pages of the given size cover items.
func PageCount(items []int, size int) int {
	if size <= 0 {
		return 0
	}
	return (len(items) + size - 1) / size
}

// LastPage returns the slice on the final page, reusing Page so the
// 1-based indexing stays consistent across the package.
func LastPage(items []int, size int) []int {
	return Page(items, PageCount(items, size), size)
}
