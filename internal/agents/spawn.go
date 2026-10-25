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

// preamble is prepended to the agent's first user message so the headless
// worker knows how to finish. Keep user-facing mentions generic: when a user
// opens the session from the agents overview it should read like a normal bee
// chat, not a themed/scaffolded worker transcript.
const agentPreamble = `You are running unattended as one of many parallel agents.
Rules:
- You are inside an isolated git worktree on a fresh branch. Commit freely.
- Make focused changes toward the task. Do not ask questions unless blocked.
- When the task is complete, end your final message with the line:
    DONE: <one-line summary>
- If you cannot proceed, end with:
    BLOCKED: <reason>
- If you need clarification mid-flight, end with:
    NEEDS-INPUT: <question>
- Once you signal DONE the coordinator rebases your branch onto main, runs
  the repo's verify commands and fast-forward merges it. If the rebase
  conflicts or verify fails you are restarted with the details. Fix it,
  commit, and signal DONE again.

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
	branch := "agents/" + id[:8] // uuid

	wt, err := WorktreePath(id)
	if err != nil {
		return SpawnResult{}, err
	}
	// remove a leftover dir if any (shouldn't exist for a fresh uuid)
	_ = os.RemoveAll(wt)

	if err := zzz.WorktreeAdd(opts.RepoRoot, wt, branch); err != nil {
		return SpawnResult{}, fmt.Errorf("worktree add: %w", err)
	}

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
	// write initial status BEFORE start so the overview row shows up
	// instantly even if the child hasn't booted yet.
	_ = bgreg.Write(pre)

	pid, logPath, err := launch(opts.RepoRoot, pre, agentPreamble+prompt)
	if err != nil {
		_ = bgreg.Remove(id)
		_ = zzz.WorktreeRemove(opts.RepoRoot, wt, true)
		return SpawnResult{}, err
	}
	pre.PID = pid
	_ = bgreg.Write(pre)

	return SpawnResult{
		SessionID:    id,
		Branch:       branch,
		WorktreePath: wt,
		PID:          pid,
		LogPath:      logPath,
	}, nil
}

// startAgent starts the detached child and returns its pid. Swapped in tests
// so they never run a real bee.
var startAgent = func(cmd *exec.Cmd) (int, error) {
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	// reap in the background: an unreaped child stays a zombie that still
	// answers signal 0, so a crashed agent would look alive to the merger.
	go func() { _ = cmd.Wait() }()
	return cmd.Process.Pid, nil
}

// launch re-execs bee in headless bg-loop mode for st's session, rooted at its
// worktree. Spawn and Resume share it so a resumed agent runs exactly like a
// fresh one.
func launch(repoRoot string, st bgreg.Status, msg string) (int, string, error) {
	self, err := os.Executable()
	if err != nil {
		return 0, "", fmt.Errorf("resolve self: %w", err)
	}
	logPath, err := logFilePath(st.SessionID)
	if err != nil {
		return 0, "", err
	}
	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, "", fmt.Errorf("open log: %w", err)
	}
	defer func() { _ = logF.Close() }()

	// spawned agents run unattended + auto-approve (--yes), so they confine to
	// the workspace by default (--safe → workspace-write): file tools stay
	// in-tree, secret paths are blocked, and the OS sandbox confines shell.
	args := []string{"run", "--headless", "--bg-loop", "--session", st.SessionID, "--yes", "--safe"}
	if st.Model != "" {
		args = append(args, "--model", st.Model)
	}
	if st.Provider != "" {
		args = append(args, "--provider", st.Provider)
	}
	args = append(args, "--", msg)

	cmd := exec.Command(self, args...)
	cmd.Dir = st.WorktreePath
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(),
		"BEE_AGENT_WORKTREE=1",
		"BEE_AGENT_BRANCH="+st.Branch,
		"BEE_AGENT_REPO_ROOT="+repoRoot,
	)
	pid, err := startAgent(cmd)
	if err != nil {
		return 0, "", fmt.Errorf("start child: %w", err)
	}
	return pid, logPath, nil
}

// logFilePath returns ~/.bee/sessions/bg/<id>.log, creating the dir.
func logFilePath(id string) (string, error) {
	p, err := bgreg.File(id, ".log")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return p, nil
}
