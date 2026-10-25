package main

import (
	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/sandbox"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/tools/dirgate"
	"github.com/elhenro/bee/internal/tools/usertool"
)

// dirGatedTools are the file tools that take a "path" argument.
var dirGatedTools = map[string]bool{
	"read": true, "ls": true, "search": true, "glob": true,
	"write": true, "edit": true, "hashline_edit": true,
}

// gateDirs wraps the file tools so they stay in cwd unless the user grants a
// directory. Unconfined scope only: a confined scope already contains them.
// No-op without an approver or when sandbox.allow_outside_cwd is set.
func gateDirs(all []tools.Tool, cwd string, cfg config.Config, confined bool, app approval.Approver) []tools.Tool {
	if confined || app == nil || cfg.Sandbox.AllowOutsideCwd {
		return all
	}
	g := dirgate.New(cwd, app, cfg.Sandbox.CommandAllowlist)
	for i, t := range all {
		if dirGatedTools[t.Spec().Name] {
			all[i] = g.Wrap(t)
		}
	}
	return all
}

// execSandbox returns the option that confines exec skills, waggles and user
// tools to the active sandbox scope. Empty under danger-full-access, so the
// default posture is unchanged.
func execSandbox(cfg config.Config, cwd string) []usertool.Option {
	w := sandbox.WrapFunc(sandbox.Policy{
		Scope:    sandbox.Scope(cfg.Sandbox.Scope),
		Approval: sandbox.ApprovalMode(cfg.Sandbox.Approval),
		Cwd:      cwd,
	})
	if w == nil {
		return nil
	}
	return []usertool.Option{usertool.WithWrap(w)}
}
