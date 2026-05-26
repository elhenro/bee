package safety

import "testing"

// Tiny seeds destructive keys + warn-on-dup. Normal/large + unknown stay zero.
func TestDefaultsForProfile_TinySeeds(t *testing.T) {
	tiny := DefaultsForProfile("tiny")
	if !tiny.WarnOnDuplicateWrites {
		t.Errorf("tiny must enable WarnOnDuplicateWrites")
	}
	want := map[string]bool{
		"rm-recursive":         true,
		"git-push-force":       true,
		"git-reset-hard":       true,
		"git-clean-force":      true,
		"git-branch-delete":    true,
		"find-delete":          true,
		"git-push-force-short": true,
	}
	got := map[string]bool{}
	for _, k := range tiny.RequireApprovalKeys {
		got[k] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("tiny RequireApprovalKeys missing %q", k)
		}
	}
}

func TestDefaultsForProfile_Empty(t *testing.T) {
	for _, name := range []string{"normal", "large", "", "unknown"} {
		ps := DefaultsForProfile(name)
		if len(ps.RequireApprovalKeys) != 0 || ps.WarnOnDuplicateWrites {
			t.Errorf("profile %q must return zero ProfileSafety, got %+v", name, ps)
		}
	}
}

// every key in tiny's default list must reference an actual DangerousPattern.
// guards against typos that silently make the feature a no-op.
func TestDefaultsForProfile_KeysAreReal(t *testing.T) {
	real := map[string]bool{}
	for _, k := range DangerousKeys() {
		real[k] = true
	}
	for _, k := range DefaultsForProfile("tiny").RequireApprovalKeys {
		if !real[k] {
			t.Errorf("tiny RequireApprovalKey %q does not match any DangerousPattern", k)
		}
	}
}
