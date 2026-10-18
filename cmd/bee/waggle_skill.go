package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/tools/skillexec"
	"github.com/elhenro/bee/internal/waggle"
)

// waggleSkill resolves name against the waggle stores: project store for cwd,
// then user store. Waggles live outside ~/.bee/skills on purpose, so they never
// enter the skill registry or the tool manifest; this is the one path that
// makes a crystallized route a `bee <name>` subcommand. Disabled routes and
// names that could escape the store dir are not resolved.
func waggleSkill(name string) (skills.Skill, bool) {
	if name == "" || strings.ContainsAny(name, `/\.`) {
		return skills.Skill{}, false
	}
	cwd, _ := os.Getwd()
	proj, errP := waggle.ProjectStore(cwd)
	user, errU := waggle.UserStore()
	for _, s := range []*waggle.Store{proj, user} {
		if s == nil || (s == proj && errP != nil) || (s == user && errU != nil) {
			continue
		}
		if !s.Exists(name) || s.IsDisabled(name) {
			continue
		}
		sk, err := skills.ParseFile(filepath.Join(s.Dir(), name+".md"))
		if err != nil {
			continue
		}
		return sk, true
	}
	return skills.Skill{}, false
}

// runExecWaggle follows a read-only exec waggle directly through the hardened
// skillexec path, no engine needed. Prints the output; exit 1 on error.
func runExecWaggle(sk skills.Skill, rest []string) {
	tool, err := skillexec.New(sk)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee %s: %v\n", sk.Name, err)
		os.Exit(1)
	}
	// skillexec parses args POSIX-style; quote each word so it stays one
	// positional parameter even with spaces.
	quoted := make([]string, len(rest))
	for i, r := range rest {
		quoted[i] = "'" + strings.ReplaceAll(r, "'", `'\''`) + "'"
	}
	res, err := tool.Run(context.Background(), map[string]any{"args": strings.Join(quoted, " ")})
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee %s: %v\n", sk.Name, err)
		os.Exit(1)
	}
	fmt.Println(strings.TrimRight(res.Content, "\n"))
	if res.IsError {
		os.Exit(1)
	}
	cwd, _ := os.Getwd()
	waggle.RecordUseByName(cwd, sk.Name, len(res.Content)/4)
}
