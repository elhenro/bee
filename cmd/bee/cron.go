package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/cron"
	"github.com/elhenro/bee/internal/zzz"
)

const cronUsage = `usage:
  bee cron ls                 list configured jobs and next run times
  bee cron run <name>         run one job now, exit non-zero on failure
  bee cron daemon             run the scheduler in the foreground until interrupted
`

// runCron dispatches `bee cron ls|run|daemon`.
func runCron(args []string) {
	fs := flag.NewFlagSet("cron", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, cronUsage) }
	if err := fs.Parse(args); err != nil || fs.NArg() == 0 {
		fs.Usage()
		os.Exit(2)
	}
	reg, err := loadCronRegistry()
	if err != nil {
		fmt.Fprintln(os.Stderr, "bee cron:", err)
		os.Exit(1)
	}
	switch sub := fs.Arg(0); sub {
	case "ls":
		cronList(reg, time.Now())
	case "run":
		if fs.NArg() != 2 {
			fs.Usage()
			os.Exit(2)
		}
		os.Exit(cronRunOne(reg, fs.Arg(1)))
	case "daemon":
		os.Exit(cronDaemon(reg))
	default:
		fmt.Fprintf(os.Stderr, "bee cron: unknown subcommand %q\n", sub)
		fs.Usage()
		os.Exit(2)
	}
}

// loadCronRegistry builds the job registry from config. Config.Load can fail
// on a missing provider key, which cron does not need, so its partial result
// is used unless it holds no jobs and a real error explains why.
func loadCronRegistry() (*cron.Registry, error) {
	cfg, loadErr := config.Load()
	if len(cfg.Cron.Jobs) == 0 && loadErr != nil {
		return nil, loadErr
	}
	reg := cron.NewRegistry()
	for _, j := range cfg.Cron.Jobs {
		_, err := reg.Add(cron.JobSpec{
			Name: j.Name, Schedule: j.Schedule, Command: j.Command,
			ActiveHours: j.ActiveHours, ResourceKey: j.ResourceKey,
		})
		if err != nil {
			return nil, err
		}
	}
	return reg, nil
}

func cronList(reg *cron.Registry, now time.Time) {
	if reg.Len() == 0 {
		fmt.Println("no cron jobs configured (add [[cron.jobs]] to ~/.bee/config.toml)")
		return
	}
	for _, j := range reg.Jobs() {
		next := "never"
		if t := j.Schedule.Next(now); !t.IsZero() {
			next = t.Format("2006-01-02 15:04")
		}
		fmt.Printf("%-20s next %s  %s\n", j.Name, next, j.Command)
	}
}

func cronRunOne(reg *cron.Registry, name string) int {
	for _, j := range reg.Jobs() {
		if j.Name != name {
			continue
		}
		if err := newCronRunner().Run(context.Background(), j); err != nil {
			fmt.Fprintf(os.Stderr, "bee cron: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(os.Stderr, "bee cron: no job named %q\n", name)
	return 1
}

func cronDaemon(reg *cron.Registry) int {
	if reg.Len() == 0 {
		fmt.Fprintln(os.Stderr, "bee cron: no jobs configured")
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	sc := cron.New(nil, newCronRunner(), reg.Jobs())
	sc.SetLogger(func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) })
	sc.Start(ctx)
	fmt.Fprintf(os.Stderr, "bee cron: daemon running %d job(s), ctrl-c to stop\n", reg.Len())
	<-ctx.Done()
	sc.Shutdown()
	return 0
}

// newCronRunner runs job commands through the shell in the current directory.
// Each run takes the zzz worktree lock for that directory, so a cron job and
// an overnight zzz run never mutate one tree at once: whichever started
// first wins and the other fails loudly. Jobs in one process run one at a
// time because they all contend for that same lock.
func newCronRunner() cron.Runner {
	var mu sync.Mutex
	return cron.RunnerFunc(func(ctx context.Context, j *cron.Job) error {
		mu.Lock()
		defer mu.Unlock()
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		// key by the repo toplevel like zzz does, or a job started from a
		// subdirectory would never contend with a zzz run on the same tree
		lockDir := wd
		if top, terr := zzz.RepoRoot(wd); terr == nil {
			lockDir = top
		}
		release, err := zzz.AcquireLock(lockDir, "cron:"+j.Name)
		if err != nil {
			return fmt.Errorf("job %s: %w", j.Name, err)
		}
		defer release()
		shell, flag := "sh", "-c"
		if runtime.GOOS == "windows" {
			shell, flag = "cmd", "/C"
		}
		cmd := exec.CommandContext(ctx, shell, flag, j.Command)
		cmd.Dir, cmd.Stdout, cmd.Stderr = wd, os.Stdout, os.Stderr
		// a job that runs `bee zzz` is a child of the lock holder, not a rival
		cmd.Env = append(os.Environ(), zzz.LockEnv(lockDir))
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("job %s: %w", j.Name, err)
		}
		return nil
	})
}
