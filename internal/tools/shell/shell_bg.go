package shell

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/tools"
)

const (
	backgroundHint = "Run in the background. Returns at once; the output arrives later as a new message."

	// a background job outlives the turn that started it, so it needs a
	// ceiling: headless runs wait for jobs and must not hang on a forgotten
	// server. timeout_seconds overrides.
	backgroundTimeout = 30 * time.Minute
)

// wantsBackground reads the flag. Small models send the string "true".
func wantsBackground(input map[string]any) bool {
	switch v := input["background"].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	}
	return false
}

// runBackground hands an already approved command to the job manager. The
// process runs on the manager's context, not the turn's, so ending or
// cancelling the turn leaves it running.
func (t *Tool) runBackground(input map[string]any, cmdStr, displayCmd, note string) tools.Result {
	if t.opts.Jobs == nil {
		return tools.Result{Content: "background jobs are not available here; run it in the foreground", IsError: true}
	}
	timeout := backgroundTimeout
	if v, ok := input["timeout_seconds"]; ok {
		secs, err := toInt(v)
		if err != nil {
			return tools.Result{Content: fmt.Sprintf("bad timeout_seconds: %v", err), IsError: true}
		}
		if secs > 0 {
			timeout = time.Duration(secs) * time.Second
		}
	}
	cwd, _ := input["cwd"].(string)
	shellBin, finalCmd := t.buildInvocation(cmdStr)

	id, err := t.opts.Jobs.Spawn(displayCmd, timeout, func(ctx context.Context, out io.Writer) *exec.Cmd {
		cmd := exec.CommandContext(ctx, shellBin, "-c", finalCmd)
		cmd.Dir = cwd
		cmd.Stdout = out
		cmd.Stderr = out
		tools.GroupKill(cmd, 2*time.Second)
		return cmd
	})
	if err != nil {
		return tools.Result{Content: fmt.Sprintf("exec error: %v", err), IsError: true}
	}
	msg := fmt.Sprintf("started job %d in the background: %s\n"+
		"its output arrives as a new message when it exits (timeout %s). keep working or end your turn; "+
		"do not sleep or poll. job tool: list, output, kill.", id, oneLine(displayCmd), timeout)
	if note != "" {
		msg = note + "\n" + msg
	}
	return tools.Result{Content: msg}
}

// oneLine flattens a multi-line command into a short status line.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 160 {
		return string(r[:160]) + "…"
	}
	return s
}
