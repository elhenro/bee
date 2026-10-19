package tui

import "strconv"

// windowRange picks the rows [start,end) of an n-row list that fit maxRows
// (floor 3), keeping sel in view and roughly centred.
func windowRange(n, sel, maxRows int) rowRange {
	if maxRows < 3 {
		maxRows = 3
	}
	if n <= maxRows {
		return rowRange{0, n}
	}
	start := max(sel-maxRows/2, 0)
	end := start + maxRows
	if end > n {
		end = n
		start = end - maxRows
	}
	return rowRange{start, end}
}

// paneRows is the row budget for a list inside a pane that spends chrome rows
// on everything else, a "+N more" footer included. A pane that doesn't know
// the terminal height (0) is not windowed. The renderer drops rows from the
// top of a frame taller than the terminal, which hides the title and the
// cursor.
func paneRows(height, chrome int) int {
	if height <= 0 {
		return 1 << 30
	}
	return height - chrome
}

// moreRow is the footer a windowed list shows, "" when everything fits.
func moreRow(shown rowRange, total int) string {
	if shown.start == 0 && shown.end == total {
		return ""
	}
	return StyleLabel.Render("  +" + strconv.Itoa(total-(shown.end-shown.start)) + " more")
}
