package read

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/elhenro/bee/internal/tools/apply_patch"
)

// readSlice emits [offset, offset+limit) and a footer with the true total.
func readSlice(f *os.File, path string, offset, limit int, hashline bool) (string, error) {
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var b strings.Builder
	line := 0
	emitted := 0
	for scanner.Scan() {
		line++
		if line < offset {
			continue
		}
		if emitted >= limit {
			// keep counting lines so the footer total is accurate.
			continue
		}
		writeLine(&b, scanner.Text(), line, hashline)
		emitted++
	}
	if err := scanErr(scanner, path); err != nil {
		return "", err
	}
	if emitted == 0 {
		if line == 0 {
			return "(empty file)", nil
		}
		return fmt.Sprintf("(no lines at offset %d; file has %d lines)", offset, line), nil
	}
	lastShown := offset + emitted - 1
	if lastShown >= line {
		fmt.Fprintf(&b, "(end of file; %d lines total)\n", line)
	} else {
		fmt.Fprintf(&b, "(showing lines %d-%d of %d; pass offset/limit to see more)\n", offset, lastShown, line)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// readTail streams the file once into a ring of size N, then emits the last
// N lines with their true 1-based line numbers. Avoids a two-pass count.
func readTail(f *os.File, path string, tail int, hashline bool) (string, error) {
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	ring := make([]string, tail)
	total := 0
	for scanner.Scan() {
		ring[total%tail] = scanner.Text()
		total++
	}
	if err := scanErr(scanner, path); err != nil {
		return "", err
	}
	if total == 0 {
		return "(empty file)", nil
	}
	first := total - tail + 1
	if first < 1 {
		first = 1
	}
	var b strings.Builder
	start := 0
	if total >= tail {
		start = total % tail
	}
	count := tail
	if total < tail {
		count = total
	}
	for i := 0; i < count; i++ {
		writeLine(&b, ring[(start+i)%tail], first+i, hashline)
	}
	fmt.Fprintf(&b, "(showing last %d of %d lines; end of file)\n", count, total)
	return strings.TrimRight(b.String(), "\n"), nil
}

// writeLine emits one prefixed line. Separator is " │ " (space-pipe-space)
// not tab so leading-tab source code can't run together with the prefix.
func writeLine(b *strings.Builder, text string, line int, hashline bool) {
	if hashline {
		fmt.Fprintf(b, "%6d#%s │ %s\n", line, apply_patch.Tag(text, line), text)
	} else {
		fmt.Fprintf(b, "%6d │ %s\n", line, text)
	}
}

// scanErr maps scanner errors to friendlier strings. The 4MB line cap is
// fine for source files but trips on huge log lines — point the model at
// shell for those instead of re-reading with bigger limits.
func scanErr(scanner *bufio.Scanner, path string) error {
	err := scanner.Err()
	if err == nil {
		return nil
	}
	if errors.Is(err, bufio.ErrTooLong) {
		return fmt.Errorf("scan %s: line exceeds 4MB buffer; use shell (head/sed/awk) for huge-line files", path)
	}
	return fmt.Errorf("scan %s: %w", path, err)
}
