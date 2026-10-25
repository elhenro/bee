package loop

import (
	"strconv"
	"strings"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/prompt"
)

// sysPromptCacheKey builds a cheap fingerprint of the inputs to prompt.Assemble.
// inputs include cwd + role + posture + caveman level + tool specs
// (name+desc/snippet hashes) + skill manifest hash + record names/body hashes +
// ctx-file paths/body hashes. content is hashed, not measured: an edit that
// keeps the length must still bust the cache. read-only is folded in because it flips the prompt prefix.
// returns "" if any required input is unstable enough to skip caching.
func sysPromptCacheKey(cfg config.Config, cwd string, role Role, readOnly bool, specs []llm.ToolSpec, skillManifest string, recs []knowledge.Record, ctxFiles []prompt.ContextFile) string {
	var b strings.Builder
	b.WriteString(cfg.DefaultProvider)
	b.WriteByte('|')
	b.WriteString(cfg.DefaultModel)
	b.WriteByte('|')
	b.WriteString(cfg.Caveman)
	b.WriteByte('|')
	b.WriteString(cwd)
	b.WriteByte('|')
	b.WriteString(string(role))
	b.WriteByte('|')
	b.WriteString(strconv.FormatBool(readOnly))
	b.WriteByte('|')
	for _, s := range specs {
		b.WriteString(s.Name)
		b.WriteByte(':')
		b.WriteString(hash8(s.Description))
		b.WriteByte(':')
		b.WriteString(hash8(s.PromptSnippet))
		b.WriteByte(';')
	}
	b.WriteByte('|')
	b.WriteString(hash8(skillManifest))
	b.WriteByte('|')
	for _, r := range recs {
		b.WriteString(r.Name)
		b.WriteByte(':')
		b.WriteString(hash8(r.Body))
		b.WriteByte(':')
		b.WriteString(strconv.FormatInt(r.Modified.UnixNano(), 10))
		b.WriteByte(';')
	}
	b.WriteByte('|')
	for _, f := range ctxFiles {
		b.WriteString(f.Path)
		b.WriteByte(':')
		b.WriteString(hash8(f.Body))
		b.WriteByte(';')
	}
	return hash8(b.String())
}
