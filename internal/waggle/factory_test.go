package waggle

import "testing"

func TestProjectManager_NonNil(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	if ProjectManager("/p") == nil {
		t.Fatal("project manager should build for a valid cwd")
	}
}

func TestProjectReplayer_NilOnColdLibrary(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	if ProjectReplayer("/p") != nil {
		t.Fatal("cold library should yield a nil replayer")
	}
}

func TestProjectReplayer_LoadsSeededRoutes(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	s, _ := ProjectStore("/p")
	seedWaggle(t, s, "r1", []Call{{Tool: "ls", Args: map[string]string{"path": "x"}}, rd("a")})
	r := ProjectReplayer("/p")
	if r == nil || r.Routes() != 1 {
		t.Fatalf("expected replayer with 1 route, got %v", r)
	}
}
