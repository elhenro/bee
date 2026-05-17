// Package prompt builds the system prompt for a bee turn.
//
// Layered assembly: caveman rules + agent identity + tool manifest +
// skills + selected memories + behavioral nudge. Honors the active
// profile's SystemPromptBudget by truncating low-priority sections.
package prompt

import (
	"fmt"
	"log"
	"strings"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/llm"
)

// no separate behavioral nudge — caveman rules cover terseness and the
// tools section is self-explanatory. every extra line costs tokens.

// Assemble builds the full system prompt.
//
// section order:
//  1. caveman rules (per cfg.Caveman level)
//  2. agent identity (2 lines)
//  3. project context (AGENTS.md/CLAUDE.md walk-up + global)
//  4. tool manifest (≤2 lines per tool, respects ToolDescChars budget)
//  5. skills manifest (one line per skill, raw input)
//  6. knowledge blocks (with staleness note when expired)
//  7. behavioral nudge
//
// budget enforcement: if total exceeds SystemPromptBudget, sections are
// dropped in this priority order — last in, first out:
//   1. drop records from the tail (lowest-priority first)
//   2. trim the skills manifest
//   3. drop context files from the tail (deepest first)
//   4. log a warning if still over.
func Assemble(
	cfg config.Config,
	regToolSpecs []llm.ToolSpec,
	skillManifest string,
	selectedRecords []knowledge.Record,
	ctxFiles []ContextFile,
) string {
	prof := config.ActiveProfile(cfg)
	level, _ := caveman.ParseLevel(cfg.Caveman)

	rules := caveman.Rules(level)
	identity := identityBlock(cfg, level)
	toolMan := toolsManifest(regToolSpecs, prof.ToolDescChars)

	// knowledge + skills + context are the trimmable sections; render then enforce budget.
	memSection := knowledgeSection(selectedRecords, prof.MemoryBodyChars)
	skillSection := skillsSection(skillManifest)
	ctxSection := renderContextSection(ctxFiles)

	out := join(rules, identity, ctxSection, toolMan, skillSection, memSection)

	budget := prof.SystemPromptBudget
	if budget <= 0 {
		return out
	}

	// trim under budget: records tail-first, then skills, then ctx files tail-first.
	for EstimateTokens(out) > budget && len(selectedRecords) > 0 {
		selectedRecords = selectedRecords[:len(selectedRecords)-1]
		memSection = knowledgeSection(selectedRecords, prof.MemoryBodyChars)
		out = join(rules, identity, ctxSection, toolMan, skillSection, memSection)
	}
	if EstimateTokens(out) > budget && skillSection != "" {
		skillSection = ""
		out = join(rules, identity, ctxSection, toolMan, skillSection, memSection)
	}
	for EstimateTokens(out) > budget && len(ctxFiles) > 0 {
		ctxFiles = ctxFiles[:len(ctxFiles)-1]
		ctxSection = renderContextSection(ctxFiles)
		out = join(rules, identity, ctxSection, toolMan, skillSection, memSection)
	}
	if EstimateTokens(out) > budget {
		log.Printf("prompt: assembled %d tokens > budget %d (model=%s profile=%s)",
			EstimateTokens(out), budget, cfg.DefaultModel, cfg.Profile)
	}
	return out
}

// EstimateTokens is a rough char/4 heuristic — fine for budget checks.
func EstimateTokens(s string) int { return len(s) / 4 }

// identityBlock is the agent identity block. Plain English with explicit
// tool-use framing when caveman is off (so small local models stay grounded
// in action verbs); terse caveman-style otherwise. cwd + git root are
// derived from the process — keeps Assemble's signature small.
func identityBlock(_ config.Config, level caveman.Level) string {
	cwd := getCwd()
	return identityBlockFor(cwd, gitRootFor(cwd), level)
}

func identityBlockFor(cwd, gitRoot string, level caveman.Level) string {
	if level == caveman.Off {
		// minimal-prompt shape: name the action verbs, ban narration. Small
		// local models (qwen, llama, ds-coder) drift into describing what
		// they'd do unless explicitly told to invoke tools.
		header := "You are the bee coding agent. You help by reading files, running shell commands, and editing or writing code. Always invoke tools to act; do not narrate intent."
		if gitRoot != "" && gitRoot != cwd {
			return fmt.Sprintf("%s\nWorking directory: %s.\nProject root: %s.", header, cwd, gitRoot)
		}
		return fmt.Sprintf("%s\nWorking directory: %s.", header, cwd)
	}
	// terse-mode identity: style rule lives in caveman rules — no duplicate
	// here. every byte counts on tiny-profile budgets.
	if gitRoot != "" && gitRoot != cwd {
		return fmt.Sprintf("bee coding agent. cwd: %s. project: %s.", cwd, gitRoot)
	}
	return fmt.Sprintf("bee coding agent. cwd: %s.", cwd)
}

// toolsManifest renders each tool as "name: snippet". When PromptSnippet is
// empty it falls back to the first line of Description, with descBudget as a
// safety cap on either source.
func toolsManifest(specs []llm.ToolSpec, descBudget int) string {
	if len(specs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Tools\n")
	for _, s := range specs {
		desc := s.PromptSnippet
		if desc == "" {
			desc = firstLine(s.Description)
		}
		if descBudget > 0 && len(desc) > descBudget {
			desc = desc[:descBudget] + "…"
		}
		fmt.Fprintf(&b, "- %s: %s\n", s.Name, desc)
	}
	return strings.TrimRight(b.String(), "\n")
}

// firstLine returns s up to the first newline (no trailing newline).
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// skillsSection wraps the registry's pre-rendered manifest.
func skillsSection(manifest string) string {
	if strings.TrimSpace(manifest) == "" {
		return ""
	}
	return "## Skills\n" + manifest
}

// knowledgeSection emits one block per record. expired records carry a
// staleness note; bodyCap > 0 truncates each body at the last word
// boundary ≤ cap and appends an ellipsis. 0 = unbounded (large profile).
func knowledgeSection(rs []knowledge.Record, bodyCap int) string {
	if len(rs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Memory\n")
	for _, r := range rs {
		age := knowledge.AgeSince(r.Modified)
		fmt.Fprintf(&b, "<memory name=%q age=%q priority=%d>\n", r.Name, age, r.Priority)
		if note := knowledge.StalenessNote(r.ExpiresAt); note != "" {
			b.WriteString(note)
			b.WriteString("\n\n")
		}
		b.WriteString(truncateBody(strings.TrimSpace(r.Body), bodyCap))
		b.WriteString("\n</memory>\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// truncateBody cuts s at the last space ≤ cap so we don't break mid-word.
// 0/negative cap means unbounded.
func truncateBody(s string, cap int) string {
	if cap <= 0 || len(s) <= cap {
		return s
	}
	cut := strings.LastIndexByte(s[:cap], ' ')
	if cut <= 0 {
		cut = cap
	}
	return s[:cut] + "…"
}

// join concatenates non-empty sections separated by a blank line.
func join(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strings.TrimRight(p, "\n"))
	}
	return b.String()
}
