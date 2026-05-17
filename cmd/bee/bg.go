// Background-task spawner. `bee bg <message>` re-execs the bee binary in
// headless mode with a pinned session id, redirects stdout+stderr to a
// log file under ~/.bee/sessions/bg/, detaches via Setsid, prints the id
// to the user, and exits.
//
// Introspection:
//   bee bg --list            list background bees
//   bee bg --tail <id>       follow a background log
//   bee bg --kill <id>       stop a background bee
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/bgreg"
	bghive "github.com/elhenro/bee/internal/hive"
	"github.com/elhenro/bee/internal/session"
)

// bgOpts captures parsed flags for the bg subcommand.
type bgOpts struct {
	Skill   string
	LogFile string
	List    bool
	Tail    string
	Kill    string
}

// parseBgArgs splits flags from the positional message. Returns a helpful
// error when no message is given so the caller can print usage.
func parseBgArgs(args []string) (string, bgOpts, error) {
	fs := flag.NewFlagSet("bg", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	skill := fs.String("skill", "", "run a skill in the background instead of a free-form message")
	logFile := fs.String("logfile", "", "override the log file path")
	list := fs.Bool("list", false, "list background bees")
	tail := fs.String("tail", "", "follow the log of a background session")
	kill := fs.String("kill", "", "kill a background bee by session id")
	if err := fs.Parse(args); err != nil {
		return "", bgOpts{}, err
	}
	msg := strings.TrimSpace(strings.Join(fs.Args(), " "))
	opts := bgOpts{Skill: *skill, LogFile: *logFile, List: *list, Tail: *tail, Kill: *kill}
	return msg, opts, nil
}

// runBg is the entry point wired into main.go's `bg` dispatch.
func runBg(args []string) {
	msg, opts, err := parseBgArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee bg: %v\n", err)
		os.Exit(2)
	}

	if opts.List {
		listBg()
		return
	}
	if opts.Tail != "" {
		tailBg(opts.Tail)
		return
	}
	if opts.Kill != "" {
		killBg(opts.Kill)
		return
	}

	// spawn mode
	if msg == "" && opts.Skill == "" {
		fmt.Fprintln(os.Stderr, "bee bg: missing <message>")
		fmt.Fprintln(os.Stderr, "usage: bee bg [--skill <name>] [--logfile <path>] <message>")
		fmt.Fprintln(os.Stderr, "       bee bg --list")
		fmt.Fprintln(os.Stderr, "       bee bg --tail <session-id>")
		fmt.Fprintln(os.Stderr, "       bee bg --kill <session-id>")
		os.Exit(2)
	}

	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee bg: resolve self: %v\n", err)
		os.Exit(1)
	}

	sessID := uuid.NewString()

	logPath := opts.LogFile
	if logPath == "" {
		p, err := bghive.EnsureLogDir(sessID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bee bg: log dir: %v\n", err)
			os.Exit(1)
		}
		logPath = p
	}

	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee bg: open log: %v\n", err)
		os.Exit(1)
	}
	defer logF.Close()

	childArgs := []string{"run", "--headless", "--bg-loop", "--session", sessID}
	if opts.Skill != "" {
		childArgs = append(childArgs, "--skill", opts.Skill)
	}
	if msg != "" {
		childArgs = append(childArgs, "--", msg)
	}

	cmd := exec.Command(self, childArgs...)
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.Stdin = nil
	detach(cmd)

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "bee bg: start: %v\n", err)
		os.Exit(1)
	}
	pid := cmd.Process.Pid

	// persist pid so --kill can find it later
	_ = bghive.WritePid(sessID, pid)

	if err := cmd.Process.Release(); err != nil {
		fmt.Fprintf(os.Stderr, "bee bg: release: %v\n", err)
	}

	fmt.Printf("session: %s\n", sessID)
	fmt.Printf("pid:     %d\n", pid)
	fmt.Printf("log:     %s\n", logPath)
	fmt.Printf("resume:  bee back %s\n", sessID)
}

// listBg prints background bees in a table.
func listBg() {
	sessions, err := session.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee bg --list: %v\n", err)
		os.Exit(1)
	}

	logs, err := listBgLogs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee bg --list: %v\n", err)
		os.Exit(1)
	}
	if len(logs) == 0 {
		fmt.Println("no background bees")
		return
	}

	logSet := make(map[string]bool)
	for _, id := range logs {
		logSet[id] = true
	}

	fmt.Printf("%-12s %-8s %-10s %s\n", "SESSION", "PID", "AGE", "PREVIEW")
	for _, s := range sessions {
		if !logSet[s.ID] {
			continue
		}
		preview, _ := session.FirstUserText(s.ID)
		if len(preview) > 50 {
			preview = preview[:47] + "..."
		}
		if preview == "" {
			preview = "(no preview)"
		}
		pid, _ := bghive.ReadPid(s.ID)
		pidStr := "-"
		if pid > 0 {
			pidStr = strconv.Itoa(pid)
		}
		sid := s.ID
		if len(sid) > 12 {
			sid = sid[:12]
		}
		fmt.Printf("%-12s %-8s %-10s %s\n", sid, pidStr, humanAge(s.Created), preview)
	}
}

// tailBg streams a background log to stdout until EOF or interrupt.
func tailBg(id string) {
	logPath, err := bghive.LogPath(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee bg --tail: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "bee bg --tail: no log for session %s\n", id)
		os.Exit(1)
	}

	f, err := os.Open(logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee bg --tail: open log: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	// seek to end and tail
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		fmt.Fprintf(os.Stderr, "bee bg --tail: seek: %v\n", err)
		os.Exit(1)
	}

	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			os.Stdout.Write(buf[:n])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			fmt.Fprintf(os.Stderr, "bee bg --tail: read: %v\n", err)
			os.Exit(1)
		}
	}
}

// killBg sends SIGTERM to a background bee and clears its sidecar files.
// Falls back to bgreg.Status.PID if the legacy hive pid file is missing.
func killBg(id string) {
	pid, err := bghive.ReadPid(id)
	if err != nil || pid <= 0 {
		if s, rerr := bgreg.Read(id); rerr == nil && s.PID > 0 {
			pid = s.PID
		}
	}
	if pid > 0 {
		if proc, perr := os.FindProcess(pid); perr == nil {
			if serr := proc.Signal(os.Interrupt); serr != nil {
				_ = proc.Kill()
			}
		}
	}
	// best-effort cleanup of bgreg sidecar + inbox so /agent list stays accurate
	_ = bgreg.Remove(id)
	_ = bgreg.InboxRemove(id)
	if pid > 0 {
		fmt.Printf("killed session %s (pid %d)\n", id, pid)
	} else {
		fmt.Printf("cleaned session %s (no live pid)\n", id)
	}
}
