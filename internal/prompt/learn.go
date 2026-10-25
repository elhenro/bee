package prompt

// learnRules makes the reasoning behind a change visible to the user. The
// `why` argument is rendered on the tool card; the closing summary is where
// the model explains what a reviewer cannot read off the diff. Off on tiny,
// which spends its budget on the task, like the `why` arg itself.
func learnRules(profile string, memory bool) string {
	if profile == "tiny" {
		return ""
	}
	rules := "## Reasons\n" +
		"- every edit/write/apply_patch call sets `why`: one line naming the bug, constraint or decision behind it, not a restatement of the diff.\n" +
		"- closing summary: say what you assumed and what you did not verify, in plain sentences."
	if memory {
		rules += "\n- after you explore an unfamiliar area, save a short knowledge_write note: what it does, entry points, one gotcha."
	}
	return rules
}
