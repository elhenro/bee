package prompt

// learnRules makes the reasoning behind a change visible to the user. The
// `why` argument is rendered on the tool card; the closing summary is where
// the model explains what a reviewer cannot read off the diff.
func learnRules(memory bool) string {
	rules := "## Reasons\n" +
		"- every edit/write/apply_patch call sets `why`: one line naming the bug, constraint or decision behind it, not a restatement of the diff.\n" +
		"- closing summary: say what you assumed and what you did not verify, in plain sentences."
	if memory {
		rules += "\n- after you explore an unfamiliar area, save a short knowledge_write note: what it does, entry points, one gotcha."
	}
	return rules
}
