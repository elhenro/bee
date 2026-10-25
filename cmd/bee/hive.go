// CLI hive lister. `bee hive` prints all known bees: background tasks
// and recent sessions, with status and a preview of the first user message.
package main

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	bghive "github.com/elhenro/bee/internal/hive"
	"github.com/elhenro/bee/internal/session"
)

// runHive lists bees on stdout and exits 0.
func runHive() {
	sessions, err := session.List()
	if err != nil {
		exitf(1, "bee hive: list sessions: %v", err)
	}

	// detect which sessions have background logs
	bgLogs, _ := listBgLogs()

	var bg, recent []sessionInfo
	for _, s := range sessions {
		preview, _ := session.FirstUserText(s.ID)
		info := sessionInfo{
			ID:      s.ID,
			Age:     humanAge(s.Created),
			Preview: ellipsize(preview, 60),
			IsBG:    slices.Contains(bgLogs, s.ID),
		}
		if info.IsBG {
			bg = append(bg, info)
		} else {
			recent = append(recent, info)
		}
	}

	// keep only most-recent non-bg sessions, capped
	if len(recent) > 15 {
		recent = recent[:15]
	}

	fmt.Printf("bee hive — %d bees\n", len(sessions))
	if len(sessions) == 0 {
		fmt.Println("  (no sessions yet)")
		return
	}

	if len(bg) > 0 {
		fmt.Printf("\nbackground bees (%d)\n", len(bg))
		for _, b := range bg {
			printBee(b)
		}
	}

	if len(recent) > 0 {
		fmt.Printf("\nrecent sessions (%d)\n", len(recent))
		for _, r := range recent {
			printBee(r)
		}
	}
}

type sessionInfo struct {
	ID      string
	Age     string
	Preview string
	IsBG    bool
}

func printBee(b sessionInfo) {
	label := " "
	if b.IsBG {
		label = "•"
	}
	fmt.Printf("  %s %s  %-10s  %s\n", label, b.ID[:min(len(b.ID), 8)], b.Age, cmp.Or(b.Preview, "(no preview)"))
}

// ellipsize cuts s to at most n runes, ending in "..." when it was cut.
func ellipsize(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-3]) + "..."
	}
	return s
}

// listBgLogs returns session ids that have a background log file.
func listBgLogs() ([]string, error) {
	dir, err := bghive.LogDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if id, ok := strings.CutSuffix(e.Name(), ".log"); ok && !e.IsDir() {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out, nil
}

func humanAge(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw ago", int(d.Hours()/24/7))
	}
}
