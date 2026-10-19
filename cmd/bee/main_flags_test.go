package main

import (
	"os"
	"reflect"
	"testing"
)

func withArgs(t *testing.T, args []string, fn func()) []string {
	t.Helper()
	old := os.Args
	t.Setenv("BEE_VERBOSE", "")
	t.Setenv("BEE_CAVEMAN", "")
	t.Setenv("BEE_EFFORT", "")
	os.Args = append([]string(nil), args...)
	defer func() { os.Args = old }()
	fn()
	return append([]string(nil), os.Args...)
}

func TestGlobalFlagsStillStrippedBeforeTerminator(t *testing.T) {
	got := withArgs(t, []string{"bee", "--verbose", "run", "--caveman", "lite", "--effort=high", "--", "msg"}, func() {
		stripVerboseFlag()
		stripCavemanFlag()
		stripEffortFlag()
	})
	if want := []string{"bee", "run", "--", "msg"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q, want %q", got, want)
	}
	if os.Getenv("BEE_VERBOSE") != "1" || os.Getenv("BEE_CAVEMAN") != "lite" || os.Getenv("BEE_EFFORT") != "high" {
		t.Fatal("stripped flags did not reach the environment")
	}
}

// words after -- are the message: `bee run -- explain --effort flag` is prose
func TestGlobalFlagsLeftAloneAfterTerminator(t *testing.T) {
	in := []string{"bee", "run", "--", "explain", "the", "--effort", "flag", "--caveman", "mode", "--verbose"}
	got := withArgs(t, in, func() {
		stripVerboseFlag()
		stripCavemanFlag()
		stripEffortFlag()
	})
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("args = %q, want unchanged %q", got, in)
	}
	if os.Getenv("BEE_VERBOSE") != "" || os.Getenv("BEE_CAVEMAN") != "" || os.Getenv("BEE_EFFORT") != "" {
		t.Fatal("flag after -- leaked into the environment")
	}
}

func TestGlobalFlagStrippedBeforeTerminatorKeepsTail(t *testing.T) {
	got := withArgs(t, []string{"bee", "--effort", "low", "run", "--", "a", "--effort", "b"}, stripEffortFlag)
	if want := []string{"bee", "run", "--", "a", "--effort", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q, want %q", got, want)
	}
}
