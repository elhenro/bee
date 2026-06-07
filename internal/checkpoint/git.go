package checkpoint

import (
	"errors"
	"os/exec"
	"strconv"
	"time"
)

// commitIdentity forces a fixed author and disables signing so snapshots never
// fail on a host with no git identity and never touch the user's git config.
var commitIdentity = []string{
	"-c", "user.name=bee",
	"-c", "user.email=bee@local",
	"-c", "commit.gpgsign=false",
	"-c", "gpg.format=",
}

// staged reports whether the index differs from HEAD (or the empty tree).
func (s *Store) staged() (bool, error) {
	_, err := s.git("diff", "--cached", "--quiet")
	if err == nil {
		return false, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return true, nil
	}
	return false, err
}

// hasHead reports whether the shadow repo has any commit yet.
func (s *Store) hasHead() bool {
	_, err := s.git("rev-parse", "--verify", "HEAD")
	return err == nil
}

// headSHA returns HEAD's object name, full or short.
func (s *Store) headSHA(full bool) (string, error) {
	if full {
		return s.git("rev-parse", "HEAD")
	}
	return s.git("rev-parse", "--short", "HEAD")
}

// setRef points ref at a full object name.
func (s *Store) setRef(ref, full string) error {
	_, err := s.git("update-ref", ref, full)
	return err
}

// nextStamp returns a strictly increasing "unix +0000" git date. Caller holds mu.
func (s *Store) nextStamp() string {
	now := time.Now().Unix()
	if now <= s.lastSec {
		now = s.lastSec + 1
	}
	s.lastSec = now
	return strconv.FormatInt(now, 10) + " +0000"
}

// commitState stages the whole work-tree and commits when it changed; when
// nothing changed it reuses HEAD. Returns the resulting state's full+short sha.
func (s *Store) commitState(msg string) (full, short string, err error) {
	if _, err = s.git("add", "-A"); err != nil {
		return
	}
	changed, err := s.staged()
	if err != nil {
		return
	}
	if !changed && s.hasHead() {
		if full, err = s.headSHA(true); err != nil {
			return
		}
		short, err = s.headSHA(false)
		return
	}
	args := append([]string{}, commitIdentity...)
	args = append(args, "commit", "-m", msg)
	if !changed {
		args = append(args, "--allow-empty") // first commit in an empty dir
	}
	// strictly increasing whole-second dates keep creatordate sort stable even
	// for snapshots taken in the same wall-clock second.
	stamp := s.nextStamp()
	env := append(s.env(), "GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp)
	if _, err = s.run(env, args...); err != nil {
		return
	}
	if full, err = s.headSHA(true); err != nil {
		return
	}
	short, err = s.headSHA(false)
	return
}
