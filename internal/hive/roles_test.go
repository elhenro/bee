package hive

import (
	"strings"
	"testing"
)

func TestAllRolesShapeAndStability(t *testing.T) {
	first := AllRoles()
	if len(first) != 11 {
		t.Fatalf("want 11 roles, got %d", len(first))
	}
	seen := make(map[Role]bool, len(first))
	for _, r := range first {
		if seen[r] {
			t.Fatalf("duplicate role %q in AllRoles", r)
		}
		seen[r] = true
	}
	second := AllRoles()
	if len(second) != len(first) {
		t.Fatalf("AllRoles length not stable: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("AllRoles order not stable at %d: %q vs %q", i, first[i], second[i])
		}
	}
	// mutation of returned slice must not leak.
	first[0] = Role("tampered")
	again := AllRoles()
	if again[0] == "tampered" {
		t.Fatal("AllRoles returned internal slice — caller mutation leaked")
	}
}

func TestEveryRoleHasNonEmptyPrompt(t *testing.T) {
	for _, r := range AllRoles() {
		p := r.SystemPrompt()
		if strings.TrimSpace(p) == "" {
			t.Errorf("role %q has empty SystemPrompt", r)
		}
		// sanity: prompts should mention the role's own name so the model
		// knows what it is.
		if !strings.Contains(strings.ToLower(p), string(r)) {
			t.Errorf("role %q prompt does not self-identify: %q", r, p)
		}
	}
}

func TestNonShellRolesRejectShell(t *testing.T) {
	noShell := []Role{
		RoleSage, RoleArchivist, RoleForager,
		RoleCritic, RoleDiviner, RoleEye, RoleScoutPlanner,
	}
	for _, r := range noShell {
		if contains(r.AllowedTools(), "bash") {
			t.Errorf("role %q must not allow bash", r)
		}
	}
}

func TestCriticAndSageAreReadOnly(t *testing.T) {
	forbidden := []string{"apply_patch", "write", "edit", "bash"}
	for _, r := range []Role{RoleCritic, RoleSage} {
		tools := r.AllowedTools()
		for _, bad := range forbidden {
			if contains(tools, bad) {
				t.Errorf("role %q must be read-only but allows %q", r, bad)
			}
		}
	}
}

func TestParseRoleCaseInsensitive(t *testing.T) {
	cases := []struct {
		in   string
		want Role
	}{
		{"queen", RoleQueen},
		{"QUEEN", RoleQueen},
		{"Queen", RoleQueen},
		{"  scout_planner  ", RoleScoutPlanner},
		{"Scout_Planner", RoleScoutPlanner},
		{"critic", RoleCritic},
	}
	for _, c := range cases {
		got, ok := ParseRole(c.in)
		if !ok {
			t.Errorf("ParseRole(%q) = false, want %q", c.in, c.want)
			continue
		}
		if got != c.want {
			t.Errorf("ParseRole(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseRoleRejectsUnknown(t *testing.T) {
	bad := []string{"", "sisyphus", "hephaestus", "foreman2", "queen-bee", "  "}
	for _, s := range bad {
		if r, ok := ParseRole(s); ok {
			t.Errorf("ParseRole(%q) = (%q, true), want false", s, r)
		}
	}
}

func TestTemperatureWithinBounds(t *testing.T) {
	for _, r := range AllRoles() {
		temp := r.Temperature()
		if temp < 0.05 || temp > 0.5 {
			t.Errorf("role %q temperature %v out of [0.05, 0.5]", r, temp)
		}
	}
}

func TestAllowedToolsReturnsCopy(t *testing.T) {
	tools := RoleQueen.AllowedTools()
	if len(tools) == 0 {
		t.Fatal("queen should have tools")
	}
	tools[0] = "tampered"
	again := RoleQueen.AllowedTools()
	if again[0] == "tampered" {
		t.Fatal("AllowedTools returned internal slice — caller mutation leaked")
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
