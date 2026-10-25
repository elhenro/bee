package knowledge

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/elhenro/bee/internal/atomicfile"
)

// slugPattern permits a tight set of leaf-name characters so the index and
// filesystem path agree round-trip.
var slugPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

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
	storeMu.Lock()
	defer storeMu.Unlock()
	final := filepath.Join(dir, leafName(r.Name))
	if err := atomicfile.Write(final, []byte(renderFile(r)), 0o600); err != nil {
		return "", err
	}
	// new file changes dir mtime on most filesystems, but invalidate
	// explicitly so a same-second write still triggers rescan.
	invalidateScanCache(dir)
	if err := rebuildIndexLocked(dir); err != nil {
		return final, fmt.Errorf("record written, index refresh failed: %w", err)
	}
	return final, nil
}

func leafName(name string) string { return strings.TrimSuffix(name, ".md") + ".md" }

func validate(e Entry) error {
	if strings.TrimSpace(e.Name) == "" {
		return errors.New("knowledge: name required")
	}
	if strings.HasPrefix(e.Name, ".") {
		return fmt.Errorf("knowledge: name %q must not start with '.'", e.Name)
	}
	// the scanner skips these two files, so a record under either name would be
	// invisible (or clobber the index); case-insensitive for folding filesystems
	switch strings.ToUpper(strings.TrimSuffix(e.Name, ".md")) {
	case "INDEX", "MEMORY":
		return fmt.Errorf("knowledge: name %q is reserved", e.Name)
	}
	if !slugPattern.MatchString(e.Name) && !slugPattern.MatchString(strings.TrimSuffix(e.Name, ".md")) {
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
	fmt.Fprintf(&b, "priority: %d\n", cmp.Or(r.Priority, DefaultPriority))
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

// yamlEscape double-quotes when the value contains YAML-sensitive characters or
// would not read back as the same string (a leading "- ", "? ", "null", ...),
// otherwise emits as-is so the common case stays readable.
func yamlEscape(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, ":#'\"\n\r\t[]{},&*!|>%@`") || strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") || !readsBackPlain(s) {
		return `"` + yamlQuoter.Replace(s) + `"`
	}
	return s
}

var yamlQuoter = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`)

// readsBackPlain reports whether s written unquoted parses back to s.
func readsBackPlain(s string) bool {
	var v struct {
		V string `yaml:"v"`
	}
	return yaml.Unmarshal([]byte("v: "+s+"\n"), &v) == nil && v.V == s
}

// rebuildIndexLocked is the body of RebuildIndex; assumes storeMu is held.
func rebuildIndexLocked(dir string) error {
	paths, err := storePaths(dir)
	if err != nil {
		return err
	}
	var rows []Entry
	for _, p := range paths {
		if e, err := ReadEntry(p); err == nil {
			e.Name = nameFromPath(p) // the index lists file names, not frontmatter names
			rows = append(rows, e)
		}
	}
	slices.SortFunc(rows, func(a, b Entry) int {
		return cmp.Or(b.Priority-a.Priority, strings.Compare(a.Name, b.Name))
	})

	var b strings.Builder
	b.WriteString("# Knowledge Index\n\n")
	if len(rows) == 0 {
		b.WriteString("_(empty)_\n")
	} else {
		b.WriteString("| name | tags | pri | desc |\n")
		b.WriteString("|------|------|-----|------|\n")
		for _, r := range rows {
			fmt.Fprintf(&b, "| %s | %s | %d | %s |\n", r.Name,
				cmp.Or(strings.Join(r.Tags, ", "), "—"), r.Priority,
				escapeTableCell(cmp.Or(r.Description, "(no description)")))
		}
	}
	return atomicfile.Write(filepath.Join(dir, IndexFileName), []byte(b.String()), 0o600)
}

var tableCellEscaper = strings.NewReplacer("\n", " ", "\r", " ", "|", `\|`)

// escapeTableCell guards the pipe-delimited table cell against bare pipes
// and newlines in user descriptions.
func escapeTableCell(s string) string { return tableCellEscaper.Replace(s) }
