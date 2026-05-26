// Tool registry construction for `bee run`. Wires the built-in tool set,
// optional approval gate, write-path filter, user-defined tools, and
// disabled-tool exclusion.
package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/tools/apply_patch"
	"github.com/elhenro/bee/internal/tools/codegraph"
	"github.com/elhenro/bee/internal/tools/edit_diff"
	"github.com/elhenro/bee/internal/tools/escalate"
	"github.com/elhenro/bee/internal/tools/find"
	"github.com/elhenro/bee/internal/tools/grep"
	"github.com/elhenro/bee/internal/tools/hashline_edit"
	"github.com/elhenro/bee/internal/tools/knowledge_search"
	"github.com/elhenro/bee/internal/tools/knowledge_write"
	"github.com/elhenro/bee/internal/tools/ls"
	"github.com/elhenro/bee/internal/tools/read"
	"github.com/elhenro/bee/internal/tools/shell"
	"github.com/elhenro/bee/internal/tools/tool_lookup"
	"github.com/elhenro/bee/internal/tools/usertool"
	"github.com/elhenro/bee/internal/tools/write"
)

// filterTools narrows reg to the comma-separated list of tool names.
// Unknown names are an error so typos fail loudly. Empty list returns reg
// unchanged.
func filterTools(reg *tools.Registry, csv string) (*tools.Registry, error) {
	want := make(map[string]bool)
	for _, name := range strings.Split(csv, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		want[name] = true
	}
	if len(want) == 0 {
		return reg, nil
	}
	out := tools.NewRegistry()
	for name := range want {
		t, ok := reg.Get(name)
		if !ok {
			return nil, fmt.Errorf("--allowed-tools: unknown tool %q", name)
		}
		if err := out.Register(t); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func buildTools(cwd string, cfg config.Config, prov llm.Provider, storeDir string) (*tools.Registry, error) {
	return buildToolsWithApprover(cwd, cfg, prov, storeDir, nil)
}

// newShellTool returns a shell tool with optional approval gating and the
// shell-environment options from cfg. nil app = no gating (matches
// pre-approval behavior).
func newShellTool(app approval.Approver, cfg config.Config) tools.Tool {
	opts := shell.Options{
		UseUserRC: cfg.Shell.UseUserRC,
		Shell:     cfg.Shell.Shell,
		RCFile:    cfg.Shell.RCFile,
	}
	if !opts.UseUserRC && opts.Shell == "" && opts.RCFile == "" {
		if app == nil {
			return shell.New()
		}
		return shell.NewWithApprover(app)
	}
	return shell.NewWithOptions(app, opts)
}

// buildHeadlessApprover wires the dangerous-command approval gate for the
// headless CLI.
//
//	autoYes=true → Static{AllowOnce}: every flagged command runs without prompt
//	                (hardline patterns still refuse).
//	autoYes=false → Cache wrapping a stdin CLI prompt. Persistent grants come
//	                from cfg.Sandbox.CommandAllowlist; AllowAlways picks append
//	                to that list on disk via PersistAllowlistEntry.
func buildHeadlessApprover(cfg config.Config, autoYes bool) approval.Approver {
	if autoYes {
		return approval.Static{Verdict: approval.AllowOnce}
	}
	cli := approval.NewCLI(os.Stdin, os.Stderr)
	// profile-level RequireApprovalKeys bypass the session AllowSession cache:
	// destructive ops re-prompt every time on tiny so a hallucinating small
	// model can't snowball one yes into a series of dangerous commands.
	return approval.NewCacheWithRequire(cli, cfg.Sandbox.CommandAllowlist,
		config.ActiveProfile(cfg).Safety.RequireApprovalKeys, PersistAllowlistEntry)
}

// buildToolsWithApprover is buildTools that wires app into the shell tool so
// safety.DetectDangerous matches consult the user before running. Pass nil to
// disable gating.
func buildToolsWithApprover(cwd string, cfg config.Config, prov llm.Provider, storeDir string, app approval.Approver) (*tools.Registry, error) {
	prof := config.ActiveProfile(cfg)
	r := tools.NewRegistry()
	all := []tools.Tool{
		newShellTool(app, cfg),
		read.NewWithLimits(prof.ReadDefaultLines, prof.ReadMaxLines),
		grep.NewWithMax(cwd, prof.GrepMaxMatches),
		find.New(cwd),
		ls.New(cwd),
		write.New(cwd),
		edit_diff.New(cwd),
		hashline_edit.New(),
		// escalate gives the model an explicit exit door — important for
		// small models that wedge on uncertain tasks instead of asking.
		escalate.New(),
	}
	// apply_patch dropped on tiny — small models mis-emit unified diffs.
	if !prof.SkipApplyPatch {
		all = append(all, apply_patch.New())
	}
	if cfg.Memory.Enabled && storeDir != "" {
		topK := cfg.Memory.TopK
		all = append(all,
			knowledge_search.New(prov, cfg.DefaultModel, storeDir, topK),
			knowledge_write.New(storeDir),
		)
	}
	all = appendCodegraphTool(all, cwd)
	all = appendUserTools(all, cfg.UserTools)
	for _, t := range all {
		if isDisabledTool(cfg.DisabledTools, t.Spec().Name) {
			continue
		}
		if err := r.Register(t); err != nil {
			return nil, err
		}
	}
	// tool_lookup registers last and reads back from r so it can answer
	// queries about every other tool, including user tools.
	if !isDisabledTool(cfg.DisabledTools, "tool_lookup") {
		if err := r.Register(tool_lookup.New(r)); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// appendCodegraphTool registers the codegraph wrapper iff the project
// has a `.codegraph/codegraph.db` index AND the `codegraph` binary is on
// PATH. Silent no-op otherwise so projects without the optional dep see
// no extra tool surface.
func appendCodegraphTool(all []tools.Tool, cwd string) []tools.Tool {
	bin, ok := codegraph.Available(cwd)
	if !ok {
		return all
	}
	return append(all, codegraph.New(cwd, bin))
}

// appendUserTools wraps each [[user_tools]] entry as a tool. Malformed
// entries (empty name/command) are silently skipped so a typo in config
// doesn't crash bootstrapping.
func appendUserTools(all []tools.Tool, ut []config.UserTool) []tools.Tool {
	for _, u := range ut {
		t, err := usertool.New(u.Name, u.Command, u.Description)
		if err != nil {
			continue
		}
		all = append(all, t)
	}
	return all
}

// isDisabledTool reports whether name appears in the disabled set.
func isDisabledTool(disabled []string, name string) bool {
	for _, d := range disabled {
		if d == name {
			return true
		}
	}
	return false
}

// buildToolsFiltered is buildTools with a path-regex constraint threaded into
// every mutation tool. Read-only tools are unaffected.
func buildToolsFiltered(cwd string, cfg config.Config, writeRe *regexp.Regexp, prov llm.Provider, storeDir string) (*tools.Registry, error) {
	return buildToolsFilteredWithApprover(cwd, cfg, writeRe, prov, storeDir, nil)
}

// buildToolsFilteredWithApprover combines buildToolsFiltered with the shell
// approval hook.
func buildToolsFilteredWithApprover(cwd string, cfg config.Config, writeRe *regexp.Regexp, prov llm.Provider, storeDir string, app approval.Approver) (*tools.Registry, error) {
	prof := config.ActiveProfile(cfg)
	r := tools.NewRegistry()
	all := []tools.Tool{
		newShellTool(app, cfg),
		read.NewWithLimits(prof.ReadDefaultLines, prof.ReadMaxLines),
		grep.NewWithMax(cwd, prof.GrepMaxMatches),
		find.New(cwd),
		ls.New(cwd),
		write.NewWithFilter(cwd, writeRe),
		edit_diff.NewWithFilter(cwd, writeRe),
		hashline_edit.NewWithFilter(writeRe),
		escalate.New(),
	}
	if !prof.SkipApplyPatch {
		all = append(all, apply_patch.NewWithFilter(writeRe))
	}
	if cfg.Memory.Enabled && storeDir != "" {
		topK := cfg.Memory.TopK
		all = append(all,
			knowledge_search.New(prov, cfg.DefaultModel, storeDir, topK),
			knowledge_write.New(storeDir),
		)
	}
	all = appendCodegraphTool(all, cwd)
	all = appendUserTools(all, cfg.UserTools)
	for _, t := range all {
		if isDisabledTool(cfg.DisabledTools, t.Spec().Name) {
			continue
		}
		if err := r.Register(t); err != nil {
			return nil, err
		}
	}
	if !isDisabledTool(cfg.DisabledTools, "tool_lookup") {
		if err := r.Register(tool_lookup.New(r)); err != nil {
			return nil, err
		}
	}
	return r, nil
}
