package loop

import "strings"

// doneSignalToken is the exact sentinel the model may emit to declare an
// open-ended task complete. Matched case-insensitively as an inline tag.
const doneSignalToken = "<promise>done</promise>"

// detectDoneSignal reports whether the assistant message contains a "DONE" sentinel
// the model may use to declare it has finished an open-ended task. Case-insensitive.
func detectDoneSignal(s string) bool {
	if s == "" {
		return false
	}
	return strings.Contains(strings.ToLower(s), doneSignalToken)
}
