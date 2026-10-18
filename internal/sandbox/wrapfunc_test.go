package sandbox

import "testing"

func TestWrapFuncUnconfinedIsNil(t *testing.T) {
	for _, s := range []Scope{"", DangerFullAccess} {
		if WrapFunc(Policy{Scope: s}) != nil {
			t.Fatalf("scope %q: want nil wrapper", s)
		}
	}
}

func TestWrapFuncUnknownScopeRefuses(t *testing.T) {
	w := WrapFunc(Policy{Scope: "bogus", Cwd: t.TempDir()})
	if w == nil {
		t.Fatal("unknown scope must still wrap")
	}
	got := w([]string{"bash", "-c", "echo hi"})
	if len(got) < 4 || got[0] != "sh" || got[3] != "bee-sandbox" {
		t.Fatalf("want refusing argv, got %v", got)
	}
}
