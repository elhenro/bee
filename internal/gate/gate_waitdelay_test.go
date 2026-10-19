//go:build !windows

package gate

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// helper process: leaves a long sleeper in its own session holding our stdout
func TestGateHelperDaemon(t *testing.T) {
	if os.Getenv("GATE_HELPER_DAEMON") != "1" {
		t.Skip("helper process")
	}
	c := exec.Command("sleep", "20")
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := c.Start(); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func TestRunReturnsDespiteDetachedGrandchild(t *testing.T) {
	unixOnly(t)
	old := pipeGrace
	pipeGrace = 300 * time.Millisecond
	defer func() { pipeGrace = old }()

	cmd := "GATE_HELPER_DAEMON=1 '" + os.Args[0] + "' -test.run=TestGateHelperDaemon"
	done := make(chan Result, 1)
	go func() {
		res, _ := Run(context.Background(), t.TempDir(), []string{cmd}, Opts{Timeout: 5 * time.Second})
		done <- res
	}()
	select {
	case res := <-done:
		if !res.OK {
			t.Fatalf("exit 0 with a lingering pipe must pass: %+v", res)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Run hung on a detached grandchild holding the output pipe")
	}
	// the heavy lock was released: another pass gets it at once
	unlock, err := lockHeavy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	unlock()
}
