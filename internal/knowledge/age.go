package knowledge

import (
	"fmt"
	"time"
)

// daysSince returns floor(days_elapsed) since t, clamped at zero so future
// or clock-skewed timestamps don't surface as negative ages.
func daysSince(t, now time.Time) int {
	d := now.Sub(t)
	if d < 0 {
		return 0
	}
	return int(d / (24 * time.Hour))
}

// AgeSince renders a short natural-language age string for modified. used
// to head each record in the assembled system prompt.
func AgeSince(modified time.Time) string {
	return ageSinceAt(modified, time.Now())
}

func ageSinceAt(modified, now time.Time) string {
	d := daysSince(modified, now)
	switch d {
	case 0:
		return "today"
	case 1:
		return "yesterday"
	default:
		return fmt.Sprintf("%d days ago", d)
	}
}

// StalenessNote returns a one-line warning if the record's expiration has
// passed. zero-value (never-expires) records always return "".
func StalenessNote(expiresAt time.Time) string {
	return stalenessNoteAt(expiresAt, time.Now())
}

func stalenessNoteAt(expiresAt, now time.Time) string {
	if expiresAt.IsZero() {
		return ""
	}
	if expiresAt.After(now) {
		return ""
	}
	d := int(now.Sub(expiresAt) / (24 * time.Hour))
	if d <= 0 {
		return "(expired today — verify before using)"
	}
	return fmt.Sprintf("(expired %d day(s) ago — verify before using)", d)
}
