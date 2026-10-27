package tui

import (
	"regexp"
	"strconv"
	"strings"
)

// one file of a unified diff, numbered and ready for rows
type patchFile struct {
	path       string
	state      string // "new", "deleted" or ""
	ops        []editOp
	adds, dels int
}

var hunkHead = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// reads a git-style unified diff. hunk headers carry the line numbers; their
// counts say where a hunk ends, so a removed line that reads `-- note` isn't
// taken for a `--- file` header
func parsePatch(patch string) []patchFile {
	var p patchParser
	for l := range strings.SplitSeq(strings.TrimRight(patch, "\n"), "\n") {
		p.line(l)
	}
	return p.files
}

type patchParser struct {
	files          []patchFile
	o, n           int // next line number on each side
	remOld, remNew int // lines the open hunk still owes
}

func (p *patchParser) file() *patchFile {
	if len(p.files) == 0 {
		p.files = append(p.files, patchFile{})
	}
	return &p.files[len(p.files)-1]
}

func (p *patchParser) line(l string) {
	if p.remOld > 0 || p.remNew > 0 {
		if p.hunkLine(l) {
			return
		}
		p.remOld, p.remNew = 0, 0
	}
	switch {
	case strings.HasPrefix(l, "diff "):
		path := ""
		if i := strings.LastIndex(l, " b/"); i >= 0 {
			path = l[i+3:]
		}
		p.files = append(p.files, patchFile{path: path})
	case strings.HasPrefix(l, "--- "):
		// a bare `---` starts the next file when the last one already has rows
		if len(p.files) == 0 || len(p.files[len(p.files)-1].ops) > 0 {
			p.files = append(p.files, patchFile{})
		}
		f := p.file()
		if name := patchName(l[4:], "a/"); name == "" {
			f.state = "new"
		} else if f.path == "" {
			f.path = name
		}
	case strings.HasPrefix(l, "+++ "):
		f := p.file()
		if name := patchName(l[4:], "b/"); name == "" {
			f.state = "deleted"
		} else {
			f.path = name
		}
	case strings.HasPrefix(l, "new file mode"):
		p.file().state = "new"
	case strings.HasPrefix(l, "deleted file mode"):
		p.file().state = "deleted"
	default:
		if m := hunkHead.FindStringSubmatch(l); m != nil {
			p.file()
			p.o, p.n = atoi(m[1]), atoi(m[3])
			p.remOld, p.remNew = hunkCount(m[2]), hunkCount(m[4])
		}
	}
}

// one body row of the open hunk; false when the line belongs to something else
func (p *patchParser) hunkLine(l string) bool {
	kind, text := byte('='), ""
	switch {
	case l == "": // editors strip the lone space off a blank context row
	case l[0] == '+':
		kind, text = '+', l[1:]
	case l[0] == '-':
		kind, text = '-', l[1:]
	case l[0] == ' ':
		text = l[1:]
	case l[0] == '\\': // "\ No newline at end of file"
		return true
	default:
		return false
	}
	f := p.file()
	f.ops = append(f.ops, editOp{kind: kind, text: text, old: p.o, new: p.n})
	switch kind {
	case '=':
		p.o++
		p.n++
		p.remOld--
		p.remNew--
	case '-':
		p.o++
		p.remOld--
		f.dels++
	case '+':
		p.n++
		p.remNew--
		f.adds++
	}
	return true
}

// path from a `---`/`+++` operand: drops the one-sided prefix and a trailing
// timestamp, "" for /dev/null
func patchName(raw, prefix string) string {
	if i := strings.IndexByte(raw, '\t'); i >= 0 {
		raw = raw[:i]
	}
	if raw == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(raw, prefix)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// hunk counts default to 1 when omitted
func hunkCount(s string) int {
	if s == "" {
		return 1
	}
	return atoi(s)
}
