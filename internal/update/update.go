// Package update probes GitHub for new commits on the bee main branch and
// applies updates by re-running install.sh in a subprocess.
//
// Used by the TUI background-checker goroutine. Two pieces are deliberately
// separated:
//
//   - Probe — cheap HTTP call, no side effects, safe to run on a timer.
//   - Apply — spawns the installer subprocess; only invoked from a user
//     decision in the modal.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// httpClient is the http client used for github probes. Override in tests by
// replacing httpClient.Transport. Timeouts are enforced via request contexts
// in Probe.
var httpClient = &http.Client{Timeout: 20 * time.Second}

// Info describes an update opportunity returned by Probe.
type Info struct {
	CurrentSHA string // build-time commit (short or full)
	LatestSHA  string // full sha of branch HEAD
	ShortSHA   string // 7-char short of LatestSHA
	Ahead      int    // commits in main not in current
	Behind     int    // commits in current not in main (forks / dirty builds)
	Repo       string // owner/repo (e.g. elhenro/bee)
	Branch     string // e.g. main
}

// Available reports whether main has commits the current build doesn't.
func (i Info) Available() bool { return i != (Info{}) && i.Ahead > 0 }

// Probe asks GitHub how far the current build's commit is behind branch HEAD.
// currentSHA can be a short or full hash. Empty currentSHA or "dev" yields
// the zero Info — dev builds don't get update prompts.
func Probe(ctx context.Context, repo, branch, currentSHA string) (Info, error) {
	if currentSHA == "" || currentSHA == "dev" {
		return Info{}, nil
	}
	if repo == "" {
		repo = defaultRepo
	}
	if branch == "" {
		branch = defaultBranch
	}

	var body struct {
		AheadBy  int `json:"ahead_by"`
		BehindBy int `json:"behind_by"`
		Commits  []struct {
			SHA string `json:"sha"`
		} `json:"commits"`
	}
	status, err := githubGet(ctx, repo, "compare/"+currentSHA+"..."+branch, &body)
	switch {
	case err != nil:
		return Info{}, err
	case status == http.StatusNotFound:
		// unknown sha (e.g. local-only commit, squash-merged history) — can't
		// compare. Fall back to a head-of-branch probe so we still detect when
		// main has clearly moved past us.
		return probeHead(ctx, repo, branch, currentSHA)
	case status != http.StatusOK:
		return Info{}, fmt.Errorf("github: status %d", status)
	}
	// compare/{base}...{head} with base=current, head=branch: ahead_by is the
	// commits head has that base lacks (new on main), behind_by the reverse.
	info := Info{
		CurrentSHA: currentSHA,
		Ahead:      body.AheadBy,
		Behind:     body.BehindBy,
		Repo:       repo,
		Branch:     branch,
	}
	if len(body.Commits) > 0 {
		info.LatestSHA = body.Commits[len(body.Commits)-1].SHA
		info.ShortSHA = shortSHA(info.LatestSHA)
	}
	return info, nil
}

// probeHead is the fallback when /compare can't resolve the current sha.
// Returns Ahead=1 with the branch tip when local sha doesn't match HEAD.
func probeHead(ctx context.Context, repo, branch, currentSHA string) (Info, error) {
	var body struct {
		SHA string `json:"sha"`
	}
	status, err := githubGet(ctx, repo, "commits/"+branch, &body)
	if err != nil {
		return Info{}, err
	}
	if status != http.StatusOK {
		return Info{}, fmt.Errorf("github head: status %d", status)
	}
	if body.SHA == "" || strings.HasPrefix(body.SHA, currentSHA) || strings.HasPrefix(currentSHA, body.SHA) {
		return Info{CurrentSHA: currentSHA, Repo: repo, Branch: branch}, nil
	}
	return Info{
		CurrentSHA: currentSHA,
		LatestSHA:  body.SHA,
		ShortSHA:   shortSHA(body.SHA),
		Ahead:      1,
		Repo:       repo,
		Branch:     branch,
	}, nil
}

// githubGet fetches repos/<repo>/<path> from the GitHub API. out is decoded
// only on a 200; any other status comes back as the code with a nil error.
func githubGet(ctx context.Context, repo, path string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repo+"/"+path, nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "bee-updater")
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("github: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return resp.StatusCode, fmt.Errorf("decode: %w", err)
	}
	return resp.StatusCode, nil
}

// shortSHA is the 7-char short of sha, or "" when sha is shorter.
func shortSHA(sha string) string {
	if len(sha) < 7 {
		return ""
	}
	return sha[:7]
}

// defaults for the repo and branch Probe and the installer fall back to.
const (
	defaultRepo   = "elhenro/bee"
	defaultBranch = "main"
)

// InstallURL is the upstream curl-piped installer script.
const InstallURL = "https://raw.githubusercontent.com/" + defaultRepo + "/" + defaultBranch + "/install.sh"

// repo and branch end up in a URL inside a shell command, so they are
// restricted to the characters git hosts allow in them; no leading dash or
// "..", which keeps them from escaping the URL path.
var (
	repoRe   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*/[A-Za-z0-9_][A-Za-z0-9._-]*$`)
	branchRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._/-]*$`)
)

// installCommand is the shell one-liner that installs from repo's branch;
// empty arguments mean upstream.
func installCommand(repo, branch string) (string, error) {
	if repo == "" {
		repo = defaultRepo
	}
	if branch == "" {
		branch = defaultBranch
	}
	if !repoRe.MatchString(repo) || !branchRe.MatchString(branch) || strings.Contains(branch, "..") {
		return "", fmt.Errorf("update: refusing install from repo %q branch %q", repo, branch)
	}
	return "curl -fsSL https://raw.githubusercontent.com/" + repo + "/" + branch + "/install.sh | sh", nil
}

// ApplyFor runs the installer of repo's branch (what Probe checked; empty =
// upstream) in a subprocess and returns its combined output. The installer
// auto-detects platform, downloads the latest binary, and installs over the
// current bee binary. Sudo is required when the install dir isn't writable,
// so this call will FAIL in that case (no tty available inside the TUI), and
// the caller is expected to surface a "run manually" hint with
// InstallCommandFor.
func ApplyFor(ctx context.Context, repo, branch string) ([]byte, error) {
	line, err := installCommand(repo, branch)
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, "sh", "-c", line).CombinedOutput()
}

// InstallCommandFor returns the shell one-liner the user can paste outside bee
// when ApplyFor fails (e.g. because /usr/local/bin needs sudo).
func InstallCommandFor(repo, branch string) string {
	line, err := installCommand(repo, branch)
	if err != nil {
		return "(invalid update_repo/update_branch in config)"
	}
	return line
}
