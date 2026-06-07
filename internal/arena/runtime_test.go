package arena

import (
	"strings"
	"testing"
)

func has(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// hasPair reports whether flag is immediately followed by val in args.
func hasPair(args []string, flag, val string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == val {
			return true
		}
	}
	return false
}

func TestDockerRunArgsHardening(t *testing.T) {
	spec := ContainerSpec{
		Name: "bee-red", Image: "bee-wars:latest", Network: "bee-combat", Alias: "red",
		Env:   map[string]string{"BEE_WARS_OPPONENT": "http://blue:8080"},
		Tmpfs: []string{"/opt/vault:size=64k"},
		Cmd:   []string{"wars-agent"},
		ReadOnly: true, MemoryMB: 1024, PidsLimit: 256, Cpus: "1",
	}
	args := dockerRunArgs(spec)

	if args[0] != "run" {
		t.Fatalf("args[0] = %q, want run", args[0])
	}
	for _, want := range []string{"-d", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--read-only"} {
		if !has(args, want) {
			t.Fatalf("missing hardening flag %q in %v", want, args)
		}
	}
	if !hasPair(args, "--name", "bee-red") {
		t.Fatal("missing --name bee-red")
	}
	if !hasPair(args, "--network", "bee-combat") || !hasPair(args, "--network-alias", "red") {
		t.Fatal("missing network wiring")
	}
	if !hasPair(args, "-e", "BEE_WARS_OPPONENT=http://blue:8080") {
		t.Fatal("missing env var")
	}
	if !hasPair(args, "--tmpfs", "/opt/vault:size=64k") {
		t.Fatal("missing tmpfs")
	}
	if !hasPair(args, "--pids-limit", "256") || !hasPair(args, "--memory", "1024m") || !hasPair(args, "--cpus", "1") {
		t.Fatalf("missing resource caps in %v", args)
	}
	// image must come before the in-container command
	img := indexOf(args, "bee-wars:latest")
	cmd := indexOf(args, "wars-agent")
	if img < 0 || cmd < 0 || img > cmd {
		t.Fatalf("image must precede cmd; img=%d cmd=%d args=%v", img, cmd, args)
	}
}

func TestDockerNetworkArgsInternal(t *testing.T) {
	args := dockerNetworkArgs("bee-combat-x", true)
	if !(args[0] == "network" && args[1] == "create") {
		t.Fatalf("want network create prefix, got %v", args)
	}
	if !has(args, "--internal") {
		t.Fatal("internal network must pass --internal (no egress)")
	}
	if !has(args, "bee-combat-x") {
		t.Fatal("network name missing")
	}
	// a non-internal network must NOT carry --internal
	if has(dockerNetworkArgs("ctrl", false), "--internal") {
		t.Fatal("non-internal network wrongly got --internal")
	}
}

func indexOf(args []string, want string) int {
	for i, a := range args {
		if a == want || strings.HasPrefix(a, want) {
			return i
		}
	}
	return -1
}
