package skills

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

// frontmatter is the raw YAML shape on disk. Kind-specific fields are
// validated after unmarshal.
type frontmatter struct {
	Name        string            `yaml:"name"`
	Type        string            `yaml:"type"`
	Description string            `yaml:"description"`
	Tools       []string          `yaml:"tools"`
	Model       string            `yaml:"model"`
	AutoApprove []string          `yaml:"auto_approve"`
	Exec        []string          `yaml:"exec"`
	Stream      bool              `yaml:"stream"`
	Server      *mcpServerRaw     `yaml:"server"`
	Endpoint    string            `yaml:"endpoint"`
	Auth        *httpAuthRaw      `yaml:"auth"`
	Env         map[string]string `yaml:"env"`
	Steps       []recipeStepRaw   `yaml:"steps"`
}

type mcpServerRaw struct {
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args"`
	Env     map[string]string `yaml:"env"`
}

type httpAuthRaw struct {
	Type   string `yaml:"type"`
	Env    string `yaml:"env"`
	Header string `yaml:"header"`
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
	fm, body, err := splitFrontmatter(raw)
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
	case KindPrompt, KindExec, KindMCP, KindHTTP, KindRecipe:
	case "":
		return Skill{}, fmt.Errorf("%s: missing type", path)
	default:
		return Skill{}, fmt.Errorf("%s: invalid type %q (want prompt|exec|mcp|http|recipe)", path, meta.Type)
	}

	s := Skill{
		Path:        path,
		Name:        meta.Name,
		Kind:        kind,
		Description: strings.TrimSpace(meta.Description),
		Tools:       meta.Tools,
		Model:       meta.Model,
		AutoApprove: meta.AutoApprove,
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
	case KindMCP:
		if meta.Server == nil || meta.Server.Command == "" {
			return Skill{}, fmt.Errorf("%s: mcp skill missing server.command", path)
		}
		s.Server = MCPServer{
			Command: meta.Server.Command,
			Args:    meta.Server.Args,
			Env:     meta.Server.Env,
		}
	case KindHTTP:
		if meta.Endpoint == "" {
			return Skill{}, fmt.Errorf("%s: http skill missing endpoint", path)
		}
		s.Endpoint = meta.Endpoint
		if meta.Auth != nil {
			s.Auth = HTTPAuth{
				Type:   meta.Auth.Type,
				Env:    meta.Auth.Env,
				Header: meta.Auth.Header,
			}
		}
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

// splitFrontmatter extracts the YAML block delimited by `---` lines at the
// top of the file. Returns (frontmatter bytes, body bytes, error).
func splitFrontmatter(raw []byte) ([]byte, []byte, error) {
	// strip optional UTF-8 BOM
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	if !bytes.HasPrefix(trimmed, []byte("---")) {
		return nil, nil, fmt.Errorf("missing frontmatter delimiter")
	}
	// drop the first --- line
	rest := trimmed[3:]
	// skip the rest of that opening line
	if i := bytes.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[i+1:]
	} else {
		return nil, nil, fmt.Errorf("malformed frontmatter")
	}
	// find closing --- on its own line
	end := findClosingDelim(rest)
	if end < 0 {
		return nil, nil, fmt.Errorf("unterminated frontmatter")
	}
	fm := rest[:end]
	body := rest[end:]
	// drop the closing --- line
	if i := bytes.IndexByte(body, '\n'); i >= 0 {
		body = body[i+1:]
	} else {
		body = nil
	}
	return fm, body, nil
}

// findClosingDelim returns the byte offset where the closing `---` line begins.
// A delimiter is a line that contains only `---` (optionally followed by \r).
func findClosingDelim(b []byte) int {
	start := 0
	for start < len(b) {
		nl := bytes.IndexByte(b[start:], '\n')
		var line []byte
		var lineEnd int
		if nl < 0 {
			line = b[start:]
			lineEnd = len(b)
		} else {
			line = b[start : start+nl]
			lineEnd = start + nl + 1
		}
		stripped := bytes.TrimRight(line, "\r ")
		if bytes.Equal(stripped, []byte("---")) {
			return start
		}
		start = lineEnd
		if nl < 0 {
			break
		}
	}
	return -1
}

// validName enforces that the skill name is usable as a filename and
// `bee <name>` subcommand.
func validName(n string) error {
	if n == "" {
		return fmt.Errorf("empty name")
	}
	for _, r := range n {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return fmt.Errorf("invalid name %q (allowed: [A-Za-z0-9_-])", n)
		}
	}
	return nil
}
