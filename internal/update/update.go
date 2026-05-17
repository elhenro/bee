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
		repo = "elhenro/bee"
	}
	if branch == "" {
		branch = "main"
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/compare/%s...%s", repo, currentSHA, branch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Info{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "bee-updater")

	resp, err := httpClient.Do(req)
	if err != nil {
		return Info{}, fmt.Errorf("github: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		// unknown sha (e.g. local-only commit, squash-merged history) — can't
		// compare. Fall back to a head-of-branch probe so we still detect when
		// main has clearly moved past us.
		return probeHead(ctx, repo, branch, currentSHA)
	}
	if resp.StatusCode != http.StatusOK {
		return Info{}, fmt.Errorf("github: status %d", resp.StatusCode)
	}
	var body struct {
		Status    string `json:"status"`
		AheadBy   int    `json:"ahead_by"`
		BehindBy  int    `json:"behind_by"`
		MergeBase struct {
			SHA string `json:"sha"`
		} `json:"merge_base_commit"`
		Commits []struct {
			SHA string `json:"sha"`
		} `json:"commits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Info{}, fmt.Errorf("decode: %w", err)
	}
	// the compare API names them from base→head. base=current, head=branch.
	// "behind_by" reports commits in head not in base — i.e. new on main.
	info := Info{
		CurrentSHA: currentSHA,
		Ahead:      body.BehindBy,
		Behind:     body.AheadBy,
		Repo:       repo,
		Branch:     branch,
	}
	if len(body.Commits) > 0 {
		info.LatestSHA = body.Commits[len(body.Commits)-1].SHA
	}
	if info.LatestSHA != "" && len(info.LatestSHA) >= 7 {
		info.ShortSHA = info.LatestSHA[:7]
	}
	return info, nil
}

// probeHead is the fallback when /compare can't resolve the current sha.
// Returns Ahead=1 with the branch tip when local sha doesn't match HEAD.
func probeHead(ctx context.Context, repo, branch, currentSHA string) (Info, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/commits/%s", repo, branch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Info{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "bee-updater")
	resp, err := httpClient.Do(req)
	if err != nil {
		return Info{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Info{}, fmt.Errorf("github head: status %d", resp.StatusCode)
	}
	var body struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Info{}, err
	}
	if body.SHA == "" || strings.HasPrefix(body.SHA, currentSHA) || strings.HasPrefix(currentSHA, body.SHA) {
		return Info{CurrentSHA: currentSHA, Repo: repo, Branch: branch}, nil
	}
	info := Info{
		CurrentSHA: currentSHA,
		LatestSHA:  body.SHA,
		Ahead:      1,
		Repo:       repo,
		Branch:     branch,
	}
	if len(body.SHA) >= 7 {
		info.ShortSHA = body.SHA[:7]
	}
	return info, nil
}

// InstallURL is the canonical curl-piped installer script.
const InstallURL = "https://raw.githubusercontent.com/elhenro/bee/main/install.sh"

// Apply runs the installer in a subprocess and returns its combined output.
// The installer auto-detects platform, downloads the latest binary, and
// installs over the current bee binary. Sudo is required when the install
// dir isn't writable — this call will FAIL in that case (no tty available
// inside the TUI), and the caller is expected to surface a "run manually"
// hint with the same curl command shown in InstallCommand.
func Apply(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", "curl -fsSL "+InstallURL+" | sh")
	return cmd.CombinedOutput()
}

// InstallCommand returns the shell one-liner the user can paste outside bee
// when Apply fails (e.g. because /usr/local/bin needs sudo).
func InstallCommand() string {
	return "curl -fsSL " + InstallURL + " | sh"
}
