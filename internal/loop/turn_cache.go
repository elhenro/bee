package loop

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/prompt"
)

// sysPromptCacheKey builds a cheap fingerprint of the inputs to prompt.Assemble.
// inputs include cwd + mode + profile + caveman level + tool specs (name+desc
// lengths) + skill manifest length + record names/sizes + ctx-file paths/lengths.
// returns "" if any required input is unstable enough to skip caching.
func sysPromptCacheKey(cfg config.Config, mode Mode, specs []llm.ToolSpec, skillManifest string, recs []knowledge.Record, ctxFiles []prompt.ContextFile) string {
	var b strings.Builder
	b.WriteString(cfg.DefaultProvider)
	b.WriteByte('|')
	b.WriteString(cfg.DefaultModel)
	b.WriteByte('|')
	b.WriteString(cfg.Profile)
	b.WriteByte('|')
	b.WriteString(cfg.Caveman)
	b.WriteByte('|')
	b.WriteString(string(mode))
	b.WriteByte('|')
	for _, s := range specs {
		b.WriteString(s.Name)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(len(s.Description)))
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(len(s.PromptSnippet)))
		b.WriteByte(';')
	}
	b.WriteByte('|')
	b.WriteString(strconv.Itoa(len(skillManifest)))
	b.WriteByte('|')
	for _, r := range recs {
		b.WriteString(r.Name)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(len(r.Body)))
		b.WriteByte(':')
		b.WriteString(strconv.FormatInt(r.Modified.UnixNano(), 10))
		b.WriteByte(';')
	}
	b.WriteByte('|')
	for _, f := range ctxFiles {
		b.WriteString(f.Path)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(len(f.Body)))
		b.WriteByte(';')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:8])
}
