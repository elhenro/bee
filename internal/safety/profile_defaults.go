package safety

// ProfileSafety is the per-profile safety calibration that bee applies on
// top of the global Sandbox + Approval config. It lets the tiny profile
// (small local models that hallucinate paths and intents) be stricter by
// default without forcing every user to hand-edit config.
type ProfileSafety struct {
	// WriteRoot is a regex relative to cwd. When set AND no CLI --write-path-re
	// is provided, edit/write/apply_patch tools refuse paths outside the root.
	WriteRoot string

	// ExtraSafeCommands extend sandbox.IsKnownSafe with profile-specific
	// known-safe shell command prefixes (e.g. "kubectl get", "docker ps").
	// merged at engine build time; never narrows the existing default list.
	ExtraSafeCommands []string

	// RequireApprovalKeys lists safety.DangerousPattern keys that must
	// always re-prompt the user, bypassing the session AllowSession cache.
	// Use for genuinely destructive operations where one "yes" should never
	// trust the model for the rest of the session.
	RequireApprovalKeys []string

	// WarnOnDuplicateWrites flips the per-Run idempotency guard. When true,
	// writing identical content to the same path twice without an intervening
	// read prepends a one-shot warning to the next tool result.
	WarnOnDuplicateWrites bool
}

// DefaultsForProfile returns the safety calibration bee ships for the named
// built-in profile. Unknown names return the zero value (no extra restrictions).
//
// Tiny seeds the destructive-git set + recursive-delete keys into
// RequireApprovalKeys so a hallucinating local model can't burn the session
// AllowSession cache to ship `rm -rf /` or `git push --force` without
// re-confirming. Local-model UX is "one yes one operation" by design.
func DefaultsForProfile(name string) ProfileSafety {
	switch name {
	case "tiny":
		return ProfileSafety{
			RequireApprovalKeys: []string{
				"rm-recursive",
				"find-delete",
				"xargs-rm",
				"find-exec-rm",
				"git-reset-hard",
				"git-push-force",
				"git-push-force-short",
				"git-clean-force",
				"git-branch-delete",
				"chown-root",
				"chmod-world-write",
			},
			WarnOnDuplicateWrites: true,
		}
	default:
		return ProfileSafety{}
	}
}
