package tools

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// PipeHeldOpen reports a clean exit whose Wait still failed with ErrWaitDelay:
// a backgrounded child kept the output pipes open, so the command itself
// succeeded.
func PipeHeldOpen(cmd *exec.Cmd, err error) bool {
	return errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success()
}

// CommandResult maps a finished command to a tool result. runCtx is the
// timeout-bound child of ctx the command ran under: its deadline alone is a
// timeout, a cancelled ctx propagates as the returned error.
func CommandResult(ctx, runCtx context.Context, err error, output string, timeout time.Duration) (Result, error) {
	timedOut := func() Result {
		return Result{Content: fmt.Sprintf("timeout after %s\n%s", timeout, output), IsError: true}
	}
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil:
		return timedOut(), nil
	case ctx.Err() != nil:
		return Result{Content: ctx.Err().Error(), IsError: true}, ctx.Err()
	case err == nil:
		return Result{Content: output}, nil
	case strings.Contains(err.Error(), "operation not permitted"):
		// cmd.Wait() may return EPERM when the process group is already dead
		// (cancel → killProcessGroup → cmd.Wait() tries another kill).
		return timedOut(), nil
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return Result{Content: fmt.Sprintf("exit %d\n%s", exitErr.ExitCode(), output), IsError: true}, nil
	}
	return Result{Content: fmt.Sprintf("exec error: %v\n%s", err, output), IsError: true}, nil
}
