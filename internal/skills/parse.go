package skills

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
)

// frontmatter is the raw YAML shape on disk. Kind-specific fields are
// validated after unmarshal.
type frontmatter struct {
	Name        string          `yaml:"name"`
	Type        string          `yaml:"type"`
	Description string          `yaml:"description"`
	Tools       []string        `yaml:"tools"`
	Exec        []string        `yaml:"exec"`
	Stream      bool            `yaml:"stream"`
	Steps       []recipeStepRaw `yaml:"steps"`
}

// ParseFile reads a *.md skill file and returns a validated Skill.
func ParseFile(path string) (Skill, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, fmt.Errorf("read %s: %w", path, err)
	}
	return Parse(path, raw)
}

// Parse parses raw frontmatter+body content. Path is informational.
func Parse(path string, raw []byte) (Skill, error) {
	fm, body, err := SplitFrontmatter(raw)
	if err != nil {
		return Skill{}, fmt.Errorf("%s: %w", path, err)
	}
	var meta frontmatter
	if err := yaml.Unmarshal(fm, &meta); err != nil {
		return Skill{}, fmt.Errorf("%s: yaml: %w", path, err)
	}

	if meta.Name == "" {
		// fall back to filename without extension
		meta.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if err := validName(meta.Name); err != nil {
		return Skill{}, fmt.Errorf("%s: %w", path, err)
	}

	kind := Kind(strings.ToLower(strings.TrimSpace(meta.Type)))
	switch kind {
	case KindPrompt, KindExec, KindRecipe:
	case "":
		return Skill{}, fmt.Errorf("%s: missing type", path)
	default:
		return Skill{}, fmt.Errorf("%s: invalid type %q (want prompt|exec|recipe)", path, meta.Type)
	}

	s := Skill{
		Path:        path,
		Name:        meta.Name,
		Kind:        kind,
		Description: strings.TrimSpace(meta.Description),
		Tools:       meta.Tools,
		Body:        strings.TrimSpace(string(body)),
	}

	switch kind {
	case KindPrompt:
		if s.Body == "" {
			return Skill{}, fmt.Errorf("%s: prompt skill has empty body", path)
		}
	case KindExec:
		if len(meta.Exec) == 0 {
			return Skill{}, fmt.Errorf("%s: exec skill missing exec[]", path)
		}
		s.Exec = meta.Exec
		s.Stream = meta.Stream
	case KindRecipe:
		// recipeBuild renders the steps into s.Body so the rest of bee
		// treats a recipe like an enriched prompt skill — no new engine
		// runtime needed. knownTools=nil at parse time (no registry); the
		// runtime catches unknown-tool calls via the existing tools.Get path.
		if err := recipeBuild(&s, meta.Steps, nil); err != nil {
			return Skill{}, fmt.Errorf("%s: %w", path, err)
		}
	}

	return s, nil
}

// SplitFrontmatter extracts the YAML block delimited by `---` lines at the
// top of the file. Returns (frontmatter bytes, body bytes, error). A closing
// delimiter is a line that contains only `---` (optionally followed by \r).
func SplitFrontmatter(raw []byte) ([]byte, []byte, error) {
	// strip optional UTF-8 BOM
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	if !bytes.HasPrefix(trimmed, []byte("---")) {
		return nil, nil, fmt.Errorf("missing frontmatter delimiter")
	}
	// skip the rest of the opening line
	_, rest, found := bytes.Cut(trimmed[3:], []byte("\n"))
	if !found {
		return nil, nil, fmt.Errorf("malformed frontmatter")
	}
	for off := 0; off < len(rest); {
		line, after, found := bytes.Cut(rest[off:], []byte("\n"))
		if string(bytes.TrimRight(line, "\r ")) == "---" {
			return rest[:off], after, nil
		}
		if !found {
			break
		}
		off = len(rest) - len(after)
	}
	return nil, nil, fmt.Errorf("unterminated frontmatter")
}

// validName enforces that the skill name is usable as a filename and
// `bee <name>` subcommand.
func validName(n string) error {
	if n == "" {
		return fmt.Errorf("empty name")
	}
	if !namePattern.MatchString(n) {
		return fmt.Errorf("invalid name %q (allowed: [A-Za-z0-9_-])", n)
	}
	return nil
}

var namePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
