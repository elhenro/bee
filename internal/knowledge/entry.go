package knowledge

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

// FrontmatterMaxLines bounds the byte budget for header parsing. anything
// past this is treated as body.
const FrontmatterMaxLines = 30

// ReadEntry stats and parses the frontmatter of one file into an Entry.
// missing or unparseable frontmatter degrades to a name-only record so the
// caller still sees the file in listings.
func ReadEntry(path string) (Entry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Entry{}, err
	}
	e := Entry{Path: path, Name: nameFromPath(path), Priority: DefaultPriority, Modified: info.ModTime()}
	fm, err := readFrontmatter(path)
	if err != nil {
		return e, nil
	}
	if name := strings.TrimSpace(fm.Name); name != "" {
		e.Name = name
	}
	e.Description = strings.TrimSpace(fm.Description)
	e.Tags = normalizeTags(fm.Tags)
	if fm.Priority != 0 {
		e.Priority = fm.Priority
	}
	if legacy := strings.TrimSpace(strings.ToLower(fm.LegacyType)); legacy != "" && len(e.Tags) == 0 {
		tag, lp := tagFromLegacyType(legacy)
		if tag != "" {
			e.Tags = []string{tag}
		}
		if fm.Priority == 0 && lp > 0 {
			e.Priority = lp
		}
	}
	if exp := strings.TrimSpace(fm.Expires); exp != "" && !strings.EqualFold(exp, "never") {
		if t, perr := parseExpires(exp); perr == nil {
			e.ExpiresAt = t
		}
	}
	return e, nil
}

func nameFromPath(p string) string {
	b := filepath.Base(p)
	return strings.TrimSuffix(b, filepath.Ext(b))
}

// tagFromLegacyType maps the deprecated four-value type field onto one of
// the well-known tag names plus a sensible default priority.
func tagFromLegacyType(t string) (string, int) {
	switch t {
	case "user":
		return TagPersonal, DefaultPriority
	case "feedback":
		return TagGuidance, 5
	case "project":
		return TagProject, DefaultPriority
	case "reference":
		return TagExternal, 2
	}
	return "", 0
}

func normalizeTags(in []string) []string {
	var out []string
	for _, t := range in {
		t = strings.TrimSpace(strings.ToLower(t))
		if t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

// parseExpires accepts RFC3339 timestamps and plain `YYYY-MM-DD` dates. a bare
// date keeps the record through that whole day.
func parseExpires(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return EndOfDay(t), nil
	}
	return time.Time{}, fmt.Errorf("unrecognized expires value %q", s)
}

// EndOfDay maps a date parsed at midnight UTC to the last second of that day,
// so "expires: D" means the record is still live all through D.
func EndOfDay(midnight time.Time) time.Time { return midnight.Add(24*time.Hour - time.Second) }

// readFrontmatter reads up to FrontmatterMaxLines and parses the YAML block
// fenced by `---`. anything missing or malformed surfaces as an error so
// the caller can degrade to a name-only entry.
func readFrontmatter(path string) (fm frontmatter, err error) {
	f, err := os.Open(path)
	if err != nil {
		return fm, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 512*1024)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return fm, errors.New("no frontmatter")
	}
	var lines []string
	for i := 1; i < FrontmatterMaxLines && sc.Scan(); i++ {
		if strings.TrimSpace(sc.Text()) == "---" {
			return fm, yaml.Unmarshal([]byte(strings.Join(lines, "\n")), &fm)
		}
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return fm, err
	}
	return fm, errors.New("unterminated frontmatter")
}

// Body returns the record's content with the frontmatter stripped.
func Body(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := string(data)
	if !strings.HasPrefix(s, "---") {
		return s, nil
	}
	_, rest, found := strings.Cut(s, "\n")
	if !found {
		return "", nil
	}
	_, body, found := strings.Cut(rest, "\n---")
	if !found {
		return rest, nil
	}
	return strings.TrimPrefix(strings.TrimPrefix(body, "\n"), "\r\n"), nil
}
