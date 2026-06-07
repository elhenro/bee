package loop

import (
	"context"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

// countingProvider records whether Stream was called, to assert the summarizer
// is skipped for short sessions.
type countingProvider struct{ onStream func() }

func (c *countingProvider) Name() string { return "counting" }
func (c *countingProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	if c.onStream != nil {
		c.onStream()
	}
	ch := make(chan llm.Event, 1)
	ch <- llm.Event{Type: llm.EventDone}
	close(ch)
	return ch, nil
}

// longHistory is a transcript with enough messages that a middle exists to
// summarize beyond the verbatim PreserveTail.
func longHistory() []types.Message {
	return []types.Message{
		mkMsg(types.RoleUser, "implement the foo parser in foo.go"),
		mkMsg(types.RoleAssistant, "reading foo.go"),
		mkMsg(types.RoleUser, "keep going"),
		mkMsg(types.RoleAssistant, "trying approach A"),
		mkMsg(types.RoleUser, "still broken"),
		mkMsg(types.RoleAssistant, "TAIL-ASSISTANT-LINE"),
	}
}

func TestBuildHandoff_IncludesGoalSummaryStallTail(t *testing.T) {
	p := &compactStubProvider{summary: "MIDDLE-SUMMARY"}
	brief, err := BuildHandoff(context.Background(), p, "stub", longHistory(), "partial-output-xyz", "format strike — model wedged")
	if err != nil {
		t.Fatal(err)
	}
	// original goal carried verbatim.
	if !strings.Contains(brief, "implement the foo parser in foo.go") {
		t.Errorf("brief must carry the original task verbatim, got:\n%s", brief)
	}
	// confused middle summarized via the provider.
	if !strings.Contains(brief, "MIDDLE-SUMMARY") {
		t.Errorf("brief must include the middle summary, got:\n%s", brief)
	}
	// stall signal surfaced.
	if !strings.Contains(brief, "format strike") {
		t.Errorf("brief must include the stall signal, got:\n%s", brief)
	}
	// verbatim tail + interrupted partial preserved.
	if !strings.Contains(brief, "TAIL-ASSISTANT-LINE") {
		t.Errorf("brief must include the verbatim tail, got:\n%s", brief)
	}
	if !strings.Contains(brief, "partial-output-xyz") {
		t.Errorf("brief must include the interrupted partial, got:\n%s", brief)
	}
}

func TestBuildHandoff_EmptyStallOmitsBlocker(t *testing.T) {
	p := &compactStubProvider{summary: "S"}
	brief, err := BuildHandoff(context.Background(), p, "stub", longHistory(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(brief, "where it got stuck") {
		t.Errorf("empty stall must omit the blocker section, got:\n%s", brief)
	}
}

func TestBuildHandoff_ShortSessionNoSummary(t *testing.T) {
	// a session at/under PreserveTail has no middle; the verbatim tail carries
	// everything and the summarizer is never called.
	called := false
	p := &countingProvider{onStream: func() { called = true }}
	msgs := []types.Message{
		mkMsg(types.RoleUser, "the original goal"),
		mkMsg(types.RoleAssistant, "first step"),
	}
	brief, err := BuildHandoff(context.Background(), p, "stub", msgs, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("short session should not invoke the summarizer")
	}
	if !strings.Contains(brief, "the original goal") {
		t.Errorf("brief must still carry the goal, got:\n%s", brief)
	}
}
