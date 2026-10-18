// Package waggle_lookup exposes the procedure-memory library to the model as a
// single tool, so the whole library costs one manifest slot instead of one per
// waggle (context stays O(1) in library size). With no name it lists the
// crystallized routes ranked by estimated tokens saved; with a name it follows
// that route, running it read-only through the hardened exec path and returning
// the output. Routes the loop replays automatically (tier 2) never need this;
// it is the on-demand path for a model that wants a proven route by intent.
package waggle_lookup

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/safety"
	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/tools/skillexec"
	"github.com/elhenro/bee/internal/tools/usertool"
	"github.com/elhenro/bee/internal/waggle"
)

const toolName = "waggle_lookup"

// Tool is the waggle_lookup tool, bound to one or more scope stores.
type Tool struct {
	stores   []*waggle.Store
	confined bool // gate path args with the secret-path guard under a confined scope
	opts     []usertool.Option
}

// New binds the tool to the given stores (typically project + user). nil stores
// are ignored. Returns nil when no store is provided.
func New(stores ...*waggle.Store) *Tool {
	var keep []*waggle.Store
	for _, s := range stores {
		if s != nil {
			keep = append(keep, s)
		}
	}
	if len(keep) == 0 {
		return nil
	}
	return &Tool{stores: keep}
}

// Confine turns on the secret-path guard for args; set it under a confined
// sandbox scope. Returns t for chaining.
func (t *Tool) Confine(confined bool) *Tool {
	t.confined = confined
	return t
}

// WithOptions passes opts (the sandbox wrapper) to every route the tool runs.
func (t *Tool) WithOptions(opts ...usertool.Option) *Tool {
	t.opts = opts
	return t
}

func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:          toolName,
		Description:   "Reuse a proven read-only route from procedure memory. Omit `name` to list available routes (ranked by tokens saved); pass `name` to run that route and get its output. Optional `args` are passed POSIX-style as positional params. Read-only and safe.",
		PromptSnippet: "list or follow a saved read-only route (waggle)",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "waggle name to follow. Omit to list available routes.",
				},
				"args": map[string]any{
					"type":        "string",
					"description": "optional positional args for a parameterized route.",
				},
			},
		},
	}
}

func (t *Tool) Run(ctx context.Context, in map[string]any) (tools.Result, error) {
	name, _ := in["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return tools.Result{Content: t.list()}, nil
	}
	return t.follow(ctx, name, in)
}

// validWaggleName reports whether name is a safe single-segment store key:
// non-empty and free of path separators or dots, so name+".md" can only resolve
// inside the store directory.
func validWaggleName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r == '/' || r == '\\' || r == '.' {
			return false
		}
	}
	return true
}

// list renders the library across stores, ranked by estimated tokens saved.
func (t *Tool) list() string {
	type row struct {
		name, desc string
		uses, yld  int
	}
	var rows []row
	for _, s := range t.stores {
		metas, _ := waggle.List(s)
		stats, _ := waggle.ReadLedger(s.LedgerPath())
		for _, m := range metas {
			st := stats[m.Name]
			desc := m.Description
			if m.Kind == string(skills.KindRecipe) {
				desc += " [recipe: bee " + m.Name + "]"
			}
			rows = append(rows, row{m.Name, desc, st.Uses, st.Yield})
		}
	}
	if len(rows) == 0 {
		return "no waggles yet"
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].yld > rows[j].yld })
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%s  (uses %d, ~%d tok saved): %s\n", r.name, r.uses, r.yld, r.desc)
	}
	return strings.TrimRight(b.String(), "\n")
}

// follow finds the named waggle in any store and runs it through skillexec,
// which re-checks safety, parses args POSIX-style, and caps output.
func (t *Tool) follow(ctx context.Context, name string, in map[string]any) (tools.Result, error) {
	// name indexes a file under the store dir (name+".md"); reject anything that
	// could escape it (/, \, .) so a crafted name can't load an arbitrary .md
	// from outside the project/user store and run it through skillexec.
	if !validWaggleName(name) {
		return tools.Result{Content: fmt.Sprintf("invalid waggle name %q", name), IsError: true}, nil
	}
	if msg := t.checkArgs(in); msg != "" {
		return tools.Result{Content: msg, IsError: true}, nil
	}
	for _, s := range t.stores {
		if !s.Exists(name) {
			continue
		}
		if s.IsDisabled(name) {
			return tools.Result{Content: fmt.Sprintf("waggle %q is disabled (demoted for repeated divergence)", name), IsError: true}, nil
		}
		sk, err := skills.ParseFile(filepath.Join(s.Dir(), name+".md"))
		if err != nil {
			return tools.Result{Content: fmt.Sprintf("waggle %q failed to parse: %v", name, err), IsError: true}, nil
		}
		if sk.Kind == skills.KindRecipe {
			// a mutating route is never run from here: it needs the approval
			// gate, which only the subcommand path provides.
			return tools.Result{Content: fmt.Sprintf("waggle %q has shell steps that change state; run it as `bee %s [args]` via the bash tool (its steps prompt for approval)", name, name), IsError: true}, nil
		}
		tool, err := skillexec.New(sk, t.opts...)
		if err != nil {
			return tools.Result{Content: fmt.Sprintf("waggle %q not runnable: %v", name, err), IsError: true}, nil
		}
		res, err := tool.Run(ctx, in)
		if err == nil && !res.IsError {
			_ = waggle.RecordUse(s, name, len(res.Content)/4)
		}
		return res, err
	}
	return tools.Result{Content: fmt.Sprintf("unknown waggle %q (omit name to list)", name), IsError: true}, nil
}

// checkArgs vets the positional args before they reach a route's command, which
// has no option terminator for every tool it runs: a dash-leading value would
// parse as a flag (find -delete) and a leading ! or ( as a find expression.
// Under a confined scope each arg must also pass the secret-path guard. Returns
// an error message, or "" when the args are fine.
func (t *Tool) checkArgs(in map[string]any) string {
	raw, _ := in["args"].(string)
	if raw == "" {
		return ""
	}
	fields, err := usertool.SplitArgs(raw)
	if err != nil {
		return fmt.Sprintf("args parse error: %v", err)
	}
	for _, f := range fields {
		if strings.HasPrefix(f, "-") || strings.HasPrefix(f, "!") || strings.HasPrefix(f, "(") {
			return fmt.Sprintf("arg %q may not start with - ! or (; prefix a path with ./", f)
		}
		if t.confined {
			if err := safety.CheckReadable(f); err != nil {
				return err.Error()
			}
		}
	}
	return ""
}
