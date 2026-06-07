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
	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/tools/skillexec"
	"github.com/elhenro/bee/internal/waggle"
)

const toolName = "waggle_lookup"

// Tool is the waggle_lookup tool, bound to one or more scope stores.
type Tool struct {
	stores []*waggle.Store
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
			rows = append(rows, row{m.Name, m.Description, st.Uses, st.Yield})
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
	for _, s := range t.stores {
		if !s.Exists(name) {
			continue
		}
		sk, err := skills.ParseFile(filepath.Join(s.Dir(), name+".md"))
		if err != nil {
			return tools.Result{Content: fmt.Sprintf("waggle %q failed to parse: %v", name, err), IsError: true}, nil
		}
		tool, err := skillexec.New(sk)
		if err != nil {
			return tools.Result{Content: fmt.Sprintf("waggle %q not runnable: %v", name, err), IsError: true}, nil
		}
		return tool.Run(ctx, in)
	}
	return tools.Result{Content: fmt.Sprintf("unknown waggle %q (omit name to list)", name), IsError: true}, nil
}
