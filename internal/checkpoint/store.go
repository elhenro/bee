// Package checkpoint snapshots the working tree into a hidden per-project git
// repo so users can rewind code to any prior turn. The shadow repo is separate
// from any real .git: its GIT_DIR lives under ~/.bee/checkpoints, its work-tree
// is the project root, and it excludes the project's own .git while honoring the
// project .gitignore. Conversation rewind is handled elsewhere (session.Fork).
package checkpoint

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Store is a shadow git repo bound to one project root.
type Store struct {
	root    string // work-tree (project root)
	gitDir  string // shadow GIT_DIR, outside the work-tree
	mu      sync.Mutex
	lastSec int64 // monotonic commit second so List ordering is stable
}

// Snapshot is one recorded code state, keyed by the message that drove the turn.
type Snapshot struct {
	MsgID string
	SHA   string
	Label string
	Time  time.Time
}

// RestoreResult reports the outcome of a rewind.
type RestoreResult struct {
	TargetSHA string // state restored to
	UndoSHA   string // pre-restore state, kept so rewind is reversible
	ShortStat string // git --shortstat between undo and target
}

// emptyTree is git's well-known empty-tree object, used for first-snapshot stats.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// Open returns the shadow store for projectRoot, initializing it on first use.
func Open(projectRoot string) (*Store, error) {
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, err
	}
	root = filepath.Clean(root)
	home, err := beeHome()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(root))
	hash := hex.EncodeToString(sum[:])[:16]
	s := &Store{
		root:   root,
		gitDir: filepath.Join(home, "checkpoints", hash, "shadow.git"),
	}
	if err := s.ensure(); err != nil {
		return nil, err
	}
	return s, nil
}

// beeHome resolves ~/.bee, overridable via BEE_HOME for hermetic tests.
func beeHome() (string, error) {
	if root := os.Getenv("BEE_HOME"); root != "" {
		return root, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".bee"), nil
}

// ensure inits the bare shadow repo and writes the .git exclude once.
func (s *Store) ensure() error {
	if _, err := os.Stat(filepath.Join(s.gitDir, "HEAD")); err == nil {
		return nil
	}
	if err := os.MkdirAll(s.gitDir, 0o755); err != nil {
		return err
	}
	if _, err := s.run(os.Environ(), "init", "--bare", s.gitDir); err != nil {
		return err
	}
	// work-tree ops need a non-bare repo even though GIT_DIR is detached.
	if _, err := s.git("config", "core.bare", "false"); err != nil {
		return err
	}
	if _, err := s.git("config", "commit.gpgsign", "false"); err != nil {
		return err
	}
	// never snapshot the project's real .git; project .gitignore still applies.
	excl := filepath.Join(s.gitDir, "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excl), 0o755); err != nil {
		return err
	}
	return os.WriteFile(excl, []byte("/.git/\n"), 0o644)
}

// env returns os.Environ with the shadow GIT_DIR/GIT_WORK_TREE forced on,
// stripping any ambient values so a parent git context cannot leak in.
func (s *Store) env() []string {
	base := os.Environ()
	out := make([]string, 0, len(base)+2)
	for _, e := range base {
		if strings.HasPrefix(e, "GIT_DIR=") || strings.HasPrefix(e, "GIT_WORK_TREE=") {
			continue
		}
		out = append(out, e)
	}
	return append(out, "GIT_DIR="+s.gitDir, "GIT_WORK_TREE="+s.root)
}

// git runs a git command against the shadow repo.
func (s *Store) git(args ...string) (string, error) {
	return s.run(s.env(), args...)
}

// run executes git with env, returning trimmed stdout; stderr only on failure.
func (s *Store) run(env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
