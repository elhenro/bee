// Package knowledge implements bee's per-project on-disk knowledge store.
//
// records are markdown files with a YAML frontmatter header carrying
// freeform tags, an explicit priority, and an optional expiration. the
// scanner returns entries cheaply (header only); record bodies are loaded
// lazily so listings stay fast even on large stores.
package knowledge

import "time"

// well-known tag names used by the migration shim when converting legacy
// type-tagged files. callers should treat these as ordinary tag strings —
// nothing in the store validates against this list.
const (
	TagPersonal = "personal"
	TagGuidance = "guidance"
	TagProject  = "project"
	TagExternal = "external"
)

// Entry is the lightweight header view of a record file: just the parsed
// frontmatter plus stat metadata. body fetched separately via Body().
type Entry struct {
	Path        string    `json:"path"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Tags        []string  `json:"tags,omitempty"`
	Priority    int       `json:"priority"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	Modified    time.Time `json:"modified"`
}

// Record is the full file: header plus body.
type Record struct {
	Entry
	Body string
}

// frontmatter is the on-disk YAML shape. legacy files may carry the old
// `type` field instead of `tags` — the parser maps it during read.
type frontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Tags        []string `yaml:"tags,omitempty"`
	Priority    int      `yaml:"priority,omitempty"`
	Expires     string   `yaml:"expires,omitempty"`
	// legacy: pre-tag taxonomy. accepted on read, never on write.
	LegacyType string `yaml:"type,omitempty"`
}

// DefaultPriority is the assumed priority when frontmatter omits the field.
const DefaultPriority = 3
