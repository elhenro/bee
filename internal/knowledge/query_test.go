package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQueryDeterministicScoring(t *testing.T) {
	dir := t.TempDir()
	must := func(name, desc string, prio int, tags ...string) {
		_, err := WriteRecord(dir, Record{
			Entry: Entry{
				Name:        name,
				Description: desc,
				Tags:        tags,
				Priority:    prio,
			},
			Body: "body of " + name,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	must("testing-policy", "test rules", 5, "testing", "guidance")
	must("user-pref", "user preferences", 3, "personal")
	must("deploy-notes", "deploy stuff", 2, "deployment")

	got, err := Query(context.Background(), dir, "how to write good tests", 5, Options{
		HintTags: []string{"testing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("want results, got 0")
	}
	if got[0].Name != "testing-policy" {
		t.Errorf("expected testing-policy first, got %+v", got)
	}
}

func TestQueryExcludesExpired(t *testing.T) {
	dir := t.TempDir()
	past := time.Now().Add(-48 * time.Hour)
	future := time.Now().Add(48 * time.Hour)
	_, _ = WriteRecord(dir, Record{
		Entry: Entry{Name: "stale", Description: "x", Tags: []string{"misc"}, Priority: 5, ExpiresAt: past},
		Body:  "stale body",
	})
	_, _ = WriteRecord(dir, Record{
		Entry: Entry{Name: "fresh", Description: "x", Tags: []string{"misc"}, Priority: 3, ExpiresAt: future},
		Body:  "fresh body",
	})
	got, err := Query(context.Background(), dir, "anything", 5, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "fresh" {
		t.Errorf("expired must be excluded, got: %+v", got)
	}
}

func TestQueryExcludeMap(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a", "b"} {
		_, _ = WriteRecord(dir, Record{
			Entry: Entry{Name: n, Description: "x", Tags: []string{"misc"}, Priority: 3},
			Body:  "body",
		})
	}
	path := filepath.Join(dir, "a.md")
	got, err := Query(context.Background(), dir, "x", 5, Options{
		Exclude: map[string]bool{path: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "b" {
		t.Errorf("exclude not respected: %+v", got)
	}
}

func TestQueryEmptyDir(t *testing.T) {
	got, err := Query(context.Background(), filepath.Join(os.TempDir(), "does-not-exist-bee"), "q", 5, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("want nil, got %+v", got)
	}
}

func TestParseTagLines(t *testing.T) {
	cases := map[string][]string{
		"testing\ndeployment\nrust":      {"testing", "deployment", "rust"},
		"- testing\n- deployment":        {"testing", "deployment"},
		"`testing`\n`deployment`":        {"testing", "deployment"},
		"":                               nil,
		"Title with Spaces\nNot allowed": nil,
		"too\nmany\ntags\nare\nclipped":  {"too", "many", "tags"},
	}
	for in, want := range cases {
		got := parseTagLines(in)
		if !slicesEqual(got, want) {
			t.Errorf("parseTagLines(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestTokenize(t *testing.T) {
	toks := tokenize("Hello world, foo-bar test123!")
	for _, want := range []string{"hello", "world", "foo-bar", "test123"} {
		if !toks[want] {
			t.Errorf("missing %q in %v", want, toks)
		}
	}
	if toks["ab"] {
		t.Errorf("short tokens should be dropped")
	}
}

func TestExtractTagsViaStubProvider(t *testing.T) {
	p := &stubKnowledgeProv{resp: "testing\ndeployment\n"}
	got, err := ExtractTags(context.Background(), p, "m", "how do I add a test for the deploy script?")
	if err != nil {
		t.Fatal(err)
	}
	if !slicesEqual(got, []string{"testing", "deployment"}) {
		t.Errorf("got %v", got)
	}
	if !strings.Contains(p.lastReq.Messages[0].Content[0].Text, "deploy") {
		t.Errorf("user text not threaded: %+v", p.lastReq)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
