package prompt

import "strings"

// efficiencyRules is the short speed ruleset for profiles that don't get the
// tiny discipline block. Targets waste seen in real runs: one tool call per
// turn during recon (the loop already runs read-only calls concurrently),
// edit-test-edit thrash with throwaway debug files, and "no evidence" answers
// to history questions that never looked at git.
func efficiencyRules(profile string) string {
	if profile == "tiny" {
		return ""
	}
	return strings.Join([]string{
		"## Efficiency",
		"- independent reads/greps go in ONE turn as parallel tool calls, not one per turn.",
		"- test fails twice on the same assertion → read that assertion and the code under it before rerunning. no throwaway debug files.",
		"- \"didn't we / was there ever\" → `git log --all -i --grep=X` and `git log -S X` first.",
	}, "\n")
}
