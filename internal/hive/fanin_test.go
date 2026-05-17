package hive

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCollect_Drains(t *testing.T) {
	ch := make(chan Result, 3)
	ch <- Result{Name: "a"}
	ch <- Result{Name: "b"}
	ch <- Result{Name: "c"}
	close(ch)
	got := Collect(ch)
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	if got[0].Name != "a" || got[2].Name != "c" {
		t.Errorf("unexpected order: %+v", got)
	}
}

func TestCollect_Empty(t *testing.T) {
	ch := make(chan Result)
	close(ch)
	if got := Collect(ch); len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}

func TestSummary_NoWorkers(t *testing.T) {
	if got := Summary(nil); !strings.Contains(got, "no workers") {
		t.Errorf("want 'no workers' in %q", got)
	}
}

func TestSummary_FormatTable(t *testing.T) {
	t0 := time.Unix(1000, 0).UTC()
	results := []Result{
		{
			Name:    "alpha",
			Final:   "hello world\nsecond line",
			Started: t0,
			Ended:   t0.Add(50 * time.Millisecond),
		},
		{
			Name:    "beta",
			Err:     errors.New("boom"),
			Started: t0.Add(10 * time.Millisecond),
			Ended:   t0.Add(60 * time.Millisecond),
		},
		{
			Name:    "gamma",
			Final:   "  ",
			Started: t0.Add(5 * time.Millisecond),
			Ended:   t0.Add(70 * time.Millisecond),
		},
	}

	got := Summary(results)
	// alpha + gamma have nil err → 2 ok; beta has err → 1 fail.
	cases := []string{
		"2 ok, 1 failed",
		"70ms wall",
		"[ok] alpha (50ms): hello world",
		"[err] beta (50ms): boom",
		"[ok] gamma (65ms): (no output)",
	}
	for _, want := range cases {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q\n--got--\n%s", want, got)
		}
	}
}

func TestSummary_TruncatesLongFinal(t *testing.T) {
	long := strings.Repeat("x", 300)
	got := Summary([]Result{{Name: "n", Final: long}})
	// one of the lines should be truncated to <=120 chars after the prefix.
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "[ok] n") && len(line) > 200 {
			t.Errorf("line not truncated: len=%d", len(line))
		}
	}
}

func TestMergeText_OnlySuccess(t *testing.T) {
	results := []Result{
		{Name: "a", Final: "alpha body\n"},
		{Name: "b", Err: errors.New("nope"), Final: "should be dropped"},
		{Name: "c", Final: "gamma body"},
	}
	got := MergeText(results)
	if !strings.Contains(got, "## a") || !strings.Contains(got, "## c") {
		t.Errorf("missing expected headers in %q", got)
	}
	if strings.Contains(got, "## b") {
		t.Errorf("failed worker leaked into merge: %q", got)
	}
	if strings.Contains(got, "should be dropped") {
		t.Errorf("failed worker body leaked: %q", got)
	}
	if !strings.Contains(got, "alpha body") || !strings.Contains(got, "gamma body") {
		t.Errorf("missing body content: %q", got)
	}
}

func TestMergeText_Empty(t *testing.T) {
	if got := MergeText(nil); got != "" {
		t.Errorf("want empty, got %q", got)
	}
	if got := MergeText([]Result{{Name: "x", Err: errors.New("e")}}); got != "" {
		t.Errorf("want empty when all failed, got %q", got)
	}
}
