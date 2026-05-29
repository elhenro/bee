package bench

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// CheckResult records one assertion's outcome.
type CheckResult struct {
	Kind   string `json:"kind"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

// RunChecks evaluates every check against sandbox. $SANDBOX in cmd/file fields
// is expanded to the sandbox path. allPassed is true only when every check
// passes (binary success — partial credit invites gaming).
func RunChecks(checks []Check, sandbox string) (results []CheckResult, allPassed bool) {
	allPassed = true
	for _, c := range checks {
		r := runCheck(c, sandbox)
		if !r.Passed {
			allPassed = false
		}
		results = append(results, r)
	}
	return results, allPassed
}

func runCheck(c Check, sandbox string) CheckResult {
	switch c.Kind {
	case "cmd":
		return runCmdCheck(c, sandbox)
	case "grep":
		return runGrepCheck(c, sandbox)
	default:
		return CheckResult{Kind: c.Kind, Passed: false, Detail: "unknown check kind"}
	}
}

func runCmdCheck(c Check, sandbox string) CheckResult {
	// cmd checks are author-written shell lines from the trusted task suite —
	// shell interpretation is the point, not an injection surface.
	line := strings.ReplaceAll(c.Run, "$SANDBOX", sandbox)
	cmd := exec.Command("sh", "-c", line)
	cmd.Env = append(os.Environ(), "SANDBOX="+sandbox)
	cmd.Dir = sandbox
	out, err := cmd.CombinedOutput()
	exit := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			return CheckResult{Kind: "cmd", Passed: false, Detail: err.Error()}
		}
	}
	passed := exit == c.ExpectExit
	detail := fmt.Sprintf("exit=%d want=%d", exit, c.ExpectExit)
	if !passed {
		detail += ": " + truncate(string(out), 200)
	}
	return CheckResult{Kind: "cmd", Passed: passed, Detail: detail}
}

func runGrepCheck(c Check, sandbox string) CheckResult {
	path := strings.ReplaceAll(c.File, "$SANDBOX", sandbox)
	re, err := regexp.Compile(c.Pattern)
	if err != nil {
		return CheckResult{Kind: "grep", Passed: false, Detail: "bad pattern: " + err.Error()}
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return CheckResult{Kind: "grep", Passed: false, Detail: err.Error()}
	}
	if re.Match(body) {
		return CheckResult{Kind: "grep", Passed: true}
	}
	return CheckResult{Kind: "grep", Passed: false, Detail: "pattern not found in " + path}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
