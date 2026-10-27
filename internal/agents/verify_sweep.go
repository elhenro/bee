package agents

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/beehome"
)

// ownerExt marks the verify checkout dir+ownerExt that records which process
// made the checkout. The marker sits beside the checkout, not in it: the gate
// runs agent code in there.
const ownerExt = ".pid"

// unmarkedMaxAge is how old a checkout without a marker (left by a build that
// wrote none) must be before it counts as abandoned. Past a verify timeout
// and the wait for the heavy lock.
const unmarkedMaxAge = 2 * time.Hour

func writeOwner(dir string) {
	_ = os.WriteFile(dir+ownerExt, []byte(strconv.Itoa(os.Getpid())), 0o600)
}

// ownerAlive reports whether the process that made the checkout still runs.
// ok=false means no readable marker.
func ownerAlive(dir string) (alive, ok bool) {
	b, err := os.ReadFile(dir + ownerExt)
	if err != nil {
		return false, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return false, false
	}
	return pidAlive(pid), true
}

// sweepVerify removes verify checkouts a killed merger never cleaned up and
// prunes the worktree entries they leave in repoRoot. A checkout is in use
// while its owner process lives.
func sweepVerify(repoRoot string) {
	base, err := beehome.Join("verify")
	if err != nil {
		return
	}
	ents, _ := os.ReadDir(base)
	for _, e := range ents {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "v-") {
			continue
		}
		dir := filepath.Join(base, e.Name())
		if alive, ok := ownerAlive(dir); alive || (!ok && !olderThan(e, unmarkedMaxAge)) {
			continue
		}
		_, _ = gitOut(repoRoot, "worktree", "remove", "--force", dir)
		_ = os.RemoveAll(dir)
		_ = os.Remove(dir + ownerExt)
	}
	_, _ = gitOut(repoRoot, "worktree", "prune")
}

func olderThan(e os.DirEntry, age time.Duration) bool {
	info, err := e.Info()
	return err == nil && time.Since(info.ModTime()) > age
}
