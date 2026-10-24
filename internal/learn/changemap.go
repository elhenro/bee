package learn

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/tools/codegraph"
	"github.com/elhenro/bee/internal/types"
)

const (
	toolTimeout = 4 * time.Second
	maxRows     = 8
)

// ChangeMap renders the end-of-turn review card for the files msgs mutated:
// line stats from git, and which tests the code index says reach each file.
// Returns "" when the turn changed nothing. Both lookups are best effort; a
// missing git repo or codegraph index just drops that part of the card.
func ChangeMap(ctx context.Context, cwd string, msgs []types.Message) string {
	changes := Touched(msgs, cwd)
	if len(changes) == 0 {
		return ""
	}
	paths := Paths(changes)
	stats := numstat(ctx, cwd, paths)
	tests, haveIndex := affectedTests(ctx, cwd, paths)

	var b strings.Builder
	fmt.Fprintf(&b, "changes %s %d %s", "·", len(changes), plural(len(changes), "file", "files"))
	for i, c := range changes {
		if i == maxRows {
			fmt.Fprintf(&b, "\n  … +%d more", len(changes)-maxRows)
			break
		}
		b.WriteString("\n  " + c.Path)
		if s, ok := stats[c.Path]; ok {
			b.WriteString("  " + s)
		}
		if len(c.Whys) > 0 {
			b.WriteString("  · " + c.Whys[len(c.Whys)-1])
		}
	}
	if haveIndex {
		if len(tests) == 0 {
			b.WriteString("\n  tests: none in the index reach these files")
		} else {
			fmt.Fprintf(&b, "\n  tests: %s", strings.Join(tests, ", "))
		}
	}
	return b.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func run(ctx context.Context, cwd, bin string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = cwd
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", false
	}
	return out.String(), true
}

// numstat maps path → "+adds −dels" from the working tree against HEAD.
func numstat(ctx context.Context, cwd string, paths []string) map[string]string {
	out, ok := run(ctx, cwd, "git", append([]string{"diff", "--numstat", "HEAD", "--"}, paths...)...)
	if !ok {
		return nil
	}
	stats := map[string]string{}
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Split(ln, "\t")
		if len(f) == 3 && f[0] != "-" {
			stats[f[2]] = "+" + f[0] + " −" + f[1]
		}
	}
	return stats
}

// affectedTests asks the code index which test files reach paths. The bool is
// false when no index is available, so the card says nothing instead of
// claiming an absence it could not check.
func affectedTests(ctx context.Context, cwd string, paths []string) ([]string, bool) {
	bin, ok := codegraph.Available(cwd)
	if !ok {
		return nil, false
	}
	out, ok := run(ctx, cwd, bin, append([]string{"affected", "-q"}, paths...)...)
	if !ok {
		return nil, false
	}
	var tests []string
	for _, ln := range strings.Split(out, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			tests = append(tests, ln)
		}
	}
	return tests, true
}
