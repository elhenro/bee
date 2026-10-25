package types

import "strings"

// syntheticPrefixes mark user-role messages the loop or compaction wrote, not
// the user.
var syntheticPrefixes = []string{"[compacted history]", "[nudge]", "[steer]", "[resume]", "[state card]", "[iter cap reached]", "[job "}

// IsSyntheticUserText reports whether text is a loop-injected turn rather than
// a real user ask.
func IsSyntheticUserText(text string) bool {
	t := strings.TrimSpace(text)
	for _, p := range syntheticPrefixes {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}
