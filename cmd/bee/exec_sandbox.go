package main

import (
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/sandbox"
	"github.com/elhenro/bee/internal/tools/usertool"
)

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
