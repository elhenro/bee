package loop

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// testFailNudgeAt is how many times one identical test failure may come back
// before the model is told to stop rerunning. a failing test piped through
// `| tail` exits 0, so repeat_detect never sees it as an error, and varying
// `-run` flags change the call signature; the failure text itself is stable.
const testFailNudgeAt = 3

var (
	testCmdRe  = regexp.MustCompile(`\b(go test|pytest|cargo test|npm (run )?test|pnpm (run )?test|vitest|jest)\b`)
	failLineRe = regexp.MustCompile(`(?m)^\s*(--- FAIL.*|\S+_test\.go:\d+:.*|FAIL\b.*|AssertionError.*|E\s{3}.*)$`)
	// timings and addresses differ per run and must not split one failure in two
	noiseRe = regexp.MustCompile(`\(\d+\.\d+s\)|\t\d+\.\d+s|0x[0-9a-f]+`)
)

// testFailFingerprint returns a stable hash of the failure lines in a test
// command's output, or "" when cmd is not a test run or nothing failed.
func testFailFingerprint(u types.ToolUse, output string) string {
	if u.Name != "bash" {
		return ""
	}
	cmd, _ := u.Input["command"].(string)
	if !testCmdRe.MatchString(cmd) {
		return ""
	}
	lines := failLineRe.FindAllString(output, -1)
	if len(lines) == 0 {
		return ""
	}
	norm := noiseRe.ReplaceAllString(strings.Join(lines, "\n"), "")
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:8])
}

// observeTestFail counts one test run's failure fingerprint and returns how
// many times that exact failure has now been seen this Run.
func (t *repeatTracker) observeTestFail(u types.ToolUse, output string) int {
	fp := testFailFingerprint(u, output)
	if fp == "" {
		return 0
	}
	if t.testFails == nil {
		t.testFails = map[string]int{}
	}
	t.testFails[fp]++
	return t.testFails[fp]
}
