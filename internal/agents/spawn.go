package agents

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/bgreg"
	"github.com/elhenro/bee/internal/zzz"
)

// SpawnOpts configures one agent spawn. Prompt is the initial user message.
// Model/Provider override the user's defaults (empty = use defaults).
type SpawnOpts struct {
	Prompt   string
	Model    string
	Provider string
	RepoRoot string // explicit; caller resolves
}

// SpawnResult is what the TUI shows after a successful spawn.
type SpawnResult struct {
	SessionID    string
	Branch       string
	WorktreePath string
	PID          int
	LogPath      string
}

// preamble is prepended to the agent's first user message so it knows it's
// running headless inside a worktree and how to signal completion.
const agentPreamble = `You are running unattended as one of many parallel bee agents.
Rules:
- You're inside an isolated git worktree on a fresh branch — commit freely.
- Make focused changes toward the task. No questions; there is no user at the keyboard.
- When the task is COMPLETE, end your final message with the line:
    DONE: <one-line summary>
- If you cannot proceed, end with:
    BLOCKED: <reason>
- If you need clarification mid-flight, end with:
    NEEDS-INPUT: <question>
- The coordinator will rebase your branch onto main and fast-forward merge it
  once you signal DONE. If the rebase conflicts you'll be re-prompted with the
  conflicting files; resolve them, commit, and signal DONE again.

Task:
`

// Spawn creates an isolated worktree, writes an initial bgreg status, then
// re-execs bee in headless bg-loop mode with cwd set to the worktree. The
// child runs detached (Setsid) so killing the TUI doesn't kill the agent.
func Spawn(opts SpawnOpts) (SpawnResult, error) {
	prompt := strings.TrimSpace(opts.Prompt)
	if prompt == "" {
		return SpawnResult{}, errors.New("agents.Spawn: empty prompt")
	}
	if opts.RepoRoot == "" {
		return SpawnResult{}, errors.New("agents.Spawn: empty RepoRoot")
	}
	// verify repo root is actually a git repo
	if _, err := zzz.RepoRoot(opts.RepoRoot); err != nil {
		return SpawnResult{}, fmt.Errorf("agents.Spawn: not a git repo: %w", err)
	}

	id := uuid.NewString()
	short := id
	if len(short) > 8 {
		short = short[:8]
	}
	branch := "agents/" + short

	wt, err := WorktreePath(id)
	if err != nil {
		return SpawnResult{}, err
	}
	// remove a leftover dir if any (shouldn't exist for a fresh uuid)
	_ = os.RemoveAll(wt)

	if err := zzz.WorktreeAdd(opts.RepoRoot, wt, branch); err != nil {
		return SpawnResult{}, fmt.Errorf("worktree add: %w", err)
	}

	self, err := os.Executable()
	if err != nil {
		_ = zzz.WorktreeRemove(opts.RepoRoot, wt, true)
		return SpawnResult{}, fmt.Errorf("resolve self: %w", err)
	}

	logPath, err := logFilePath(id)
	if err != nil {
		_ = zzz.WorktreeRemove(opts.RepoRoot, wt, true)
		return SpawnResult{}, err
	}
	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		_ = zzz.WorktreeRemove(opts.RepoRoot, wt, true)
		return SpawnResult{}, fmt.Errorf("open log: %w", err)
	}

	args := []string{"run", "--headless", "--bg-loop", "--session", id, "--yes"}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Provider != "" {
		args = append(args, "--provider", opts.Provider)
	}
	args = append(args, "--", agentPreamble+prompt)

	cmd := exec.Command(self, args...)
	cmd.Dir = wt
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(),
		"BEE_AGENT_WORKTREE=1",
		"BEE_AGENT_BRANCH="+branch,
		"BEE_AGENT_REPO_ROOT="+opts.RepoRoot,
	)
	detach(cmd)

	// write initial status BEFORE start so the overview row shows up
	// instantly even if the child hasn't booted yet.
	pre := bgreg.Status{
		SchemaV:      2,
		SessionID:    id,
		State:        bgreg.StateActive,
		Task:         prompt,
		Model:        opts.Model,
		Provider:     opts.Provider,
		Cwd:          wt,
		WorktreePath: wt,
		Branch:       branch,
		MergeState:   bgreg.MergeStateUnmerged,
		StartedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	_ = bgreg.Write(pre)

	if err := cmd.Start(); err != nil {
		_ = logF.Close()
		_ = bgreg.Remove(id)
		_ = zzz.WorktreeRemove(opts.RepoRoot, wt, true)
		return SpawnResult{}, fmt.Errorf("start child: %w", err)
	}
	pid := cmd.Process.Pid

	// update status with PID
	pre.PID = pid
	_ = bgreg.Write(pre)

	if err := cmd.Process.Release(); err != nil {
		// non-fatal; child is already running
		fmt.Fprintf(os.Stderr, "agents.Spawn: release: %v\n", err)
	}
	_ = logF.Close()

	return SpawnResult{
		SessionID:    id,
		Branch:       branch,
		WorktreePath: wt,
		PID:          pid,
		LogPath:      logPath,
	}, nil
}

// logFilePath returns ~/.bee/sessions/bg/<id>.log, creating the dir.
func logFilePath(id string) (string, error) {
	h, err := beeHome()
	if err != nil {
		return "", err
	}
	d := filepath.Join(h, "sessions", "bg")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(d, id+".log"), nil
}
