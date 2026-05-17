package knowledge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// slugChar permits a tight set of leaf-name characters so the index and
// filesystem path agree round-trip.
var slugChar = func() func(string) bool {
	allowed := map[byte]bool{}
	for c := byte('a'); c <= 'z'; c++ {
		allowed[c] = true
	}
	for c := byte('A'); c <= 'Z'; c++ {
		allowed[c] = true
	}
	for c := byte('0'); c <= '9'; c++ {
		allowed[c] = true
	}
	allowed['-'] = true
	allowed['_'] = true
	allowed['.'] = true
	return func(s string) bool {
		if s == "" {
			return false
		}
		for i := 0; i < len(s); i++ {
			if !allowed[s[i]] {
				return false
			}
		}
		return true
	}
}()

// tagPattern enforces lowercase-alphanumeric plus hyphen.
var tagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// WriteRecord persists r to dir/<r.Name>.md. validates frontmatter, writes
// via a temp file + rename so a crash never leaves a partial record, and
// finally refreshes the INDEX. returns the absolute path of the new file.
func WriteRecord(dir string, r Record) (string, error) {
	if err := validate(r.Entry); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	leaf := leafName(r.Name)
	final := filepath.Join(dir, leaf)
	tmp, err := os.CreateTemp(dir, leaf+".*.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if _, err := tmp.WriteString(renderFile(r)); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", err
	}
	if err := os.Rename(tmpPath, final); err != nil {
		cleanup()
		return "", err
	}
	if err := RebuildIndex(dir); err != nil {
		return final, fmt.Errorf("record written, index refresh failed: %w", err)
	}
	return final, nil
}

func leafName(name string) string {
	if strings.HasSuffix(name, ".md") {
		return name
	}
	return name + ".md"
}

func validate(e Entry) error {
	if strings.TrimSpace(e.Name) == "" {
		return errors.New("knowledge: name required")
	}
	if !slugChar(e.Name) && !slugChar(strings.TrimSuffix(e.Name, ".md")) {
		return fmt.Errorf("knowledge: name %q must be a slug (a-z, 0-9, -, _, .)", e.Name)
	}
	if strings.TrimSpace(e.Description) == "" {
		return errors.New("knowledge: description required")
	}
	if e.Priority != 0 && (e.Priority < 1 || e.Priority > 5) {
		return fmt.Errorf("knowledge: priority %d out of range (1-5)", e.Priority)
	}
	if len(e.Tags) > 5 {
		return fmt.Errorf("knowledge: at most 5 tags (got %d)", len(e.Tags))
	}
	for _, t := range e.Tags {
		if !tagPattern.MatchString(t) {
			return fmt.Errorf("knowledge: tag %q must be lowercase alphanumeric with hyphens", t)
		}
	}
	return nil
}

func renderFile(r Record) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", yamlEscape(r.Name))
	fmt.Fprintf(&b, "description: %s\n", yamlEscape(r.Description))
	if len(r.Tags) > 0 {
		fmt.Fprintf(&b, "tags: [%s]\n", strings.Join(r.Tags, ", "))
	}
	prio := r.Priority
	if prio == 0 {
		prio = DefaultPriority
	}
	fmt.Fprintf(&b, "priority: %d\n", prio)
	if r.ExpiresAt.IsZero() {
		b.WriteString("expires: never\n")
	} else {
		fmt.Fprintf(&b, "expires: %s\n", r.ExpiresAt.UTC().Format(time.RFC3339))
	}
	b.WriteString("---\n\n")
	body := strings.TrimRight(r.Body, "\n")
	if body != "" {
		b.WriteString(body)
		b.WriteByte('\n')
	}
	return b.String()
}

// yamlEscape double-quotes when the value contains YAML-sensitive characters,
// otherwise emits as-is so the common case stays readable.
func yamlEscape(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, ":#'\"\n\r\t[]{},&*!|>%@`") || strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		escaped := strings.ReplaceAll(s, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		escaped = strings.ReplaceAll(escaped, "\n", `\n`)
		escaped = strings.ReplaceAll(escaped, "\r", `\r`)
		escaped = strings.ReplaceAll(escaped, "\t", `\t`)
		return `"` + escaped + `"`
	}
	return s
}

// RebuildIndex rewrites INDEX.md as a markdown table sorted by priority
// desc then name asc. callers run this after each WriteRecord but may also
// invoke directly to repair an out-of-sync index.
func RebuildIndex(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type row struct {
		name     string
		tags     string
		priority int
		desc     string
	}
	var rows []row
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if e.Name() == IndexFileName || e.Name() == "MEMORY.md" {
			continue
		}
		full := filepath.Join(dir, e.Name())
		ent, err := ReadEntry(full)
		if err != nil {
			continue
		}
		desc := ent.Description
		if desc == "" {
			desc = "(no description)"
		}
		rows = append(rows, row{
			name:     strings.TrimSuffix(e.Name(), ".md"),
			tags:     strings.Join(ent.Tags, ", "),
			priority: ent.Priority,
			desc:     desc,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].priority != rows[j].priority {
			return rows[i].priority > rows[j].priority
		}
		return rows[i].name < rows[j].name
	})

	var b strings.Builder
	b.WriteString("# Knowledge Index\n\n")
	if len(rows) == 0 {
		b.WriteString("_(empty)_\n")
	} else {
		b.WriteString("| name | tags | pri | desc |\n")
		b.WriteString("|------|------|-----|------|\n")
		for _, r := range rows {
			tags := r.tags
			if tags == "" {
				tags = "—"
			}
			fmt.Fprintf(&b, "| %s | %s | %d | %s |\n", r.name, tags, r.priority, escapeTableCell(r.desc))
		}
	}
	indexPath := filepath.Join(dir, IndexFileName)
	tmp, err := os.CreateTemp(dir, IndexFileName+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(b.String()); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, indexPath)
}

// escapeTableCell guards the pipe-delimited table cell against bare pipes
// and newlines in user descriptions.
func escapeTableCell(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "|", `\|`)
	return s
}
