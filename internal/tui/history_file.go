package tui

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/elhenro/bee/internal/atomicfile"
	"github.com/elhenro/bee/internal/beehome"
)

// historyMax caps the on-disk history file. Sized for years of prompts so
// fish-style autosuggestions can match against the user's whole history.
// A var so tests can shrink it.
var historyMax = 50000

var (
	historyMu sync.Mutex
	// lines in the file at historyCountPath, counted once per process and then
	// bumped per append, so a submit never rescans the file to decide on a trim
	historyCount     int
	historyCountPath string
)

// historyPath returns ~/.bee/history (honors BEE_HOME), "" when unresolvable.
func historyPath() string {
	p, _ := beehome.Join("history")
	return p
}

// readHistoryLines returns every line of the history file, nil when unreadable.
func readHistoryLines(p string) []string {
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

// LoadHistory reads ~/.bee/history newest-first, dedupes consecutive entries.
func LoadHistory() []string {
	p := historyPath()
	if p == "" {
		return nil
	}
	historyMu.Lock()
	defer historyMu.Unlock()
	lines := readHistoryLines(p)
	// reverse to newest-first and dedupe globally — most-recent occurrence
	// wins so autosuggestions surface the freshest match for any prefix.
	out := make([]string, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, line := range slices.Backward(lines) {
		if line == "" {
			continue
		}
		l := decodeHistory(line)
		if _, ok := seen[l]; ok {
			continue
		}
		seen[l] = struct{}{}
		out = append(out, l)
	}
	return out
}

// multiMark prefixes a stored entry that held newlines. Plain single-line
// entries are stored untouched, so older history files load unchanged.
const multiMark = "\x1e"

var (
	historyEscape   = strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\r", `\r`)
	historyUnescape = strings.NewReplacer(`\\`, `\`, `\n`, "\n", `\r`, "\r")
)

// encodeHistory folds a multi-line prompt onto one file line.
func encodeHistory(t string) string {
	if !strings.ContainsAny(t, "\n\r") {
		return t
	}
	return multiMark + historyEscape.Replace(t)
}

func decodeHistory(l string) string {
	if rest, ok := strings.CutPrefix(l, multiMark); ok {
		return historyUnescape.Replace(rest)
	}
	return l
}

// AppendHistory atomically appends a single entry to ~/.bee/history. Skips
// empty / whitespace-only lines. Trims to historyMax if the file gets large.
// The file is owner-only: prompts can contain secrets.
func AppendHistory(text string) {
	t := strings.TrimSpace(text)
	if t == "" {
		return
	}
	p := historyPath()
	if p == "" {
		return
	}
	historyMu.Lock()
	defer historyMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	if historyCountPath != p {
		historyCount, historyCountPath = countHistoryLines(p), p
		_ = os.Chmod(p, 0o600) // files from older versions were world-readable
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = f.WriteString(encodeHistory(t) + "\n")
	_ = f.Close()
	historyCount++
	if historyCount > historyMax {
		trimHistory(p)
	}
}

// countHistoryLines counts newline-terminated lines without building them.
func countHistoryLines(p string) int {
	f, err := os.Open(p)
	if err != nil {
		return 0
	}
	defer f.Close()
	n, buf := 0, make([]byte, 1<<16)
	for {
		c, err := f.Read(buf)
		n += bytes.Count(buf[:c], []byte{'\n'})
		if err != nil {
			return n
		}
	}
}

// trimHistory shrinks the file to its newest historyMax lines. Only reached
// once the in-memory count passes the cap, so the full read is rare.
func trimHistory(p string) {
	lines := readHistoryLines(p)
	historyCount = len(lines)
	if len(lines) <= historyMax {
		return
	}
	keep := strings.Join(lines[len(lines)-historyMax:], "\n") + "\n"
	if atomicfile.Write(p, []byte(keep), 0o600) == nil {
		historyCount = historyMax
	}
}
