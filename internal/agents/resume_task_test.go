//go:build !windows

package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/bgreg"
	"github.com/elhenro/bee/internal/config"
)

func TestResumeRestatesWholeTask(t *testing.T) {
	f := newFixture(t)
	long := strings.Repeat("Implement the feature with great care. ", 20) + "FINAL-REQUIREMENT-TAILMARK"
	f.set(t, func(s *bgreg.Status) { s.Task = long })
	f.conflict(t)
	TryMerge(context.Background(), f.repo, f.status(t), config.SwarmConfig{})
	if len(f.starts) != 1 || !strings.Contains(f.starts[0], "FINAL-REQUIREMENT-TAILMARK") {
		t.Fatalf("resume prompt lost the tail of the task: %q", f.starts)
	}
}
