package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// withLookPath swaps the package lookPath for a test, restoring on cleanup.
func withLookPath(t *testing.T, stub func(string) (string, error)) {
	t.Helper()
	orig := lookPath
	lookPath = stub
	t.Cleanup(func() { lookPath = orig })
}

func TestWrap_DangerFullAccess_NoWrapping(t *testing.T) {
	cmd := []string{"rm", "-rf", "/tmp/x"}
	got, err := Wrap(Policy{Scope: DangerFullAccess}, cmd)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !equalSlice(got, cmd) {
		t.Errorf("got %v, want pass-through %v", got, cmd)
	}
}

func TestWrap_EmptyCmd(t *testing.T) {
	if _, err := Wrap(Policy{Scope: ReadOnly}, nil); err == nil {
		t.Error("expected error for empty cmd")
	}
}

func TestWrap_HelperMissing_DegradesGracefully(t *testing.T) {
	// stub LookPath to always fail — simulates bwrap/sandbox-exec not installed
	withLookPath(t, func(string) (string, error) {
		return "", errors.New("not found")
	})
	cmd := []string{"ls", "-la"}
	p := Policy{Scope: ReadOnly, Cwd: "/tmp"}
	got, err := Wrap(p, cmd)

	if runtime.GOOS == "windows" {
		// windows stub returns ErrUnsupported regardless of helper state
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("windows err = %v, want ErrUnsupported", err)
		}
	} else if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		if !errors.Is(err, ErrHelperMissing) {
			t.Fatalf("err = %v, want ErrHelperMissing", err)
		}
	}
	// regardless of platform, graceful-degrade returns the original cmd
	if !equalSlice(got, cmd) {
		t.Errorf("got %v, want original cmd %v", got, cmd)
	}
}

func TestWrap_MacOS_ReadOnly(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only")
	}
	withLookPath(t, func(string) (string, error) { return "/usr/bin/sandbox-exec", nil })
	cmd := []string{"ls", "/"}
	got, err := Wrap(Policy{Scope: ReadOnly}, cmd)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got[0] != "sandbox-exec" || got[1] != "-p" {
		t.Errorf("got %v, want sandbox-exec -p ...", got)
	}
	if !strings.Contains(got[2], "(deny default)") {
		t.Errorf("profile missing deny default: %q", got[2])
	}
	if !strings.Contains(got[2], "(deny network*)") {
		t.Errorf("profile missing deny network: %q", got[2])
	}
	if !equalSlice(got[3:], cmd) {
		t.Errorf("inner cmd %v != %v", got[3:], cmd)
	}
}

func TestWrap_MacOS_WorkspaceWrite_RequiresCwd(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only")
	}
	withLookPath(t, func(string) (string, error) { return "/usr/bin/sandbox-exec", nil })
	if _, err := Wrap(Policy{Scope: WorkspaceWrite}, []string{"ls"}); err == nil {
		t.Error("expected error for missing Cwd")
	}
	got, err := Wrap(Policy{Scope: WorkspaceWrite, Cwd: "/tmp/work"}, []string{"ls"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(got[2], `"/tmp/work"`) {
		t.Errorf("profile missing cwd: %q", got[2])
	}
}

func TestWrap_Linux_ReadOnly(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	withLookPath(t, func(string) (string, error) { return "/usr/bin/bwrap", nil })
	cmd := []string{"ls", "/"}
	got, err := Wrap(Policy{Scope: ReadOnly}, cmd)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got[0] != "bwrap" {
		t.Fatalf("got %v, want bwrap first", got)
	}
	joined := strings.Join(got, " ")
	for _, want := range []string{"--ro-bind / /", "--proc /proc", "--dev /dev", "--unshare-net"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing flag %q in %s", want, joined)
		}
	}
}

func TestWrap_Linux_WorkspaceWrite_BindsCwd(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	withLookPath(t, func(string) (string, error) { return "/usr/bin/bwrap", nil })
	got, err := Wrap(Policy{Scope: WorkspaceWrite, Cwd: "/work"}, []string{"go", "test"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "--bind /work /work") {
		t.Errorf("missing writable bind: %s", joined)
	}
	if !strings.Contains(joined, "--chdir /work") {
		t.Errorf("missing chdir: %s", joined)
	}
}

func TestWrap_Windows_Stub(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	got, err := Wrap(Policy{Scope: ReadOnly}, []string{"dir"})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
	if !equalSlice(got, []string{"dir"}) {
		t.Errorf("got %v, want pass-through", got)
	}
}

func TestWrap_MacOS_WorkspaceWrite_IncludesSymlinkAlias(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only")
	}
	withLookPath(t, func(string) (string, error) { return "/usr/bin/sandbox-exec", nil })

	// real dir + symlink pointing at it: both paths must appear in the
	// profile so seatbelt allows writes regardless of which form the
	// operand canonicalizes to.
	realDir := t.TempDir()
	linkDir := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	got, err := Wrap(Policy{Scope: WorkspaceWrite, Cwd: linkDir}, []string{"ls"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	profile := got[2]
	// the symlink form (literal cwd) must appear, and at least one
	// resolved form must appear that differs from it. exact canonical
	// path varies (macOS firmlinks /var → /private/var), so just check
	// that we emitted more than one subpath alias for the cwd.
	if !strings.Contains(profile, `"`+linkDir+`"`) {
		t.Errorf("profile missing literal cwd %q\n%s", linkDir, profile)
	}
	aliases := cwdAliases(linkDir)
	if len(aliases) < 2 {
		t.Fatalf("expected >=2 aliases for symlinked cwd, got %v", aliases)
	}
	for _, a := range aliases {
		if !strings.Contains(profile, `"`+a+`"`) {
			t.Errorf("profile missing alias %q\n%s", a, profile)
		}
	}
	_ = realDir
}

func TestCwdAliases_DedupesWhenIdentical(t *testing.T) {
	dir := t.TempDir()
	got := cwdAliases(dir)
	for i, p := range got {
		for j := i + 1; j < len(got); j++ {
			if p == got[j] {
				t.Errorf("duplicate entry %q at %d,%d in %v", p, i, j, got)
			}
		}
	}
}

func TestHelperPath_NonEmpty(t *testing.T) {
	p, err := HelperPath()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.HasSuffix(p, "bee-sandbox-helper") {
		t.Errorf("HelperPath() = %q, want suffix bee-sandbox-helper", p)
	}
}

func TestIsHelper(t *testing.T) {
	// We can't easily mutate os.Args[0] portably; just smoke-test that the
	// function returns a bool without panicking on the current argv.
	_ = IsHelper()
}

func TestHelperMain_PlaceholderErrors(t *testing.T) {
	if err := HelperMain(); err == nil {
		t.Error("HelperMain should error until wave 2 implements it")
	}
}

func equalSlice(a, b []string) bool {
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
