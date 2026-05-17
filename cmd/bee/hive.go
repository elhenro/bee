// CLI hive lister. `bee hive` prints all known bees: background tasks
// and recent sessions, with status and a preview of the first user message.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	bghive "github.com/elhenro/bee/internal/hive"
	"github.com/elhenro/bee/internal/session"
)

// runHive lists bees on stdout and exits 0.
func runHive(args []string) {
	_ = args

	sessions, err := session.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee hive: list sessions: %v\n", err)
		os.Exit(1)
	}

	// detect which sessions have background logs
	bgSet := make(map[string]bool)
	bgLogs, _ := listBgLogs()
	for _, id := range bgLogs {
		bgSet[id] = true
	}

	var bg, recent []sessionInfo
	for _, s := range sessions {
		preview, _ := session.FirstUserText(s.ID)
		if len(preview) > 60 {
			preview = preview[:57] + "..."
		}
		info := sessionInfo{
			ID:      s.ID,
			Age:     humanAge(s.Created),
			Preview: preview,
			IsBG:    bgSet[s.ID],
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
			printBee(b, true)
		}
	}

	if len(recent) > 0 {
		fmt.Printf("\nrecent sessions (%d)\n", len(recent))
		for _, r := range recent {
			printBee(r, false)
		}
	}
}

type sessionInfo struct {
	ID      string
	Age     string
	Preview string
	IsBG    bool
}

func printBee(b sessionInfo, isBG bool) {
	label := " "
	if isBG {
		label = "•"
	}
	preview := b.Preview
	if preview == "" {
		preview = "(no preview)"
	}
	sid := b.ID
	if len(sid) > 8 {
		sid = sid[:8]
	}
	fmt.Printf("  %s %s  %-10s  %s\n", label, sid, b.Age, preview)
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
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".log") {
			continue
		}
		id := strings.TrimSuffix(name, ".log")
		out = append(out, id)
	}
	sort.Strings(out)
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
