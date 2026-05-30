package ask

import (
	"context"
	"testing"
)

func TestStatic_PicksRecommended(t *testing.T) {
	q := Question{Options: []Option{
		{Label: "a"},
		{Label: "b", Recommended: true},
		{Label: "c"},
	}}
	ans, _ := Static{}.Ask(context.Background(), q)
	if ans.Index != 1 || ans.Text != "b" {
		t.Fatalf("got %+v, want index 1 label b", ans)
	}
}

func TestStatic_FallsBackToFirst(t *testing.T) {
	q := Question{Options: []Option{{Label: "a"}, {Label: "b"}}}
	ans, _ := Static{}.Ask(context.Background(), q)
	if ans.Index != 0 || ans.Text != "a" {
		t.Fatalf("got %+v, want first option", ans)
	}
}

func TestStatic_EmptyDismisses(t *testing.T) {
	ans, _ := Static{}.Ask(context.Background(), Question{})
	if !ans.Dismissed {
		t.Fatalf("empty options should dismiss, got %+v", ans)
	}
}
