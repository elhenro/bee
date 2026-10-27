package main

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuf is a bytes.Buffer safe to read while the child process writes it.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// startRemote runs `bee remote-control args...` until it prints its ready line,
// then kills it and returns everything written so far.
func startRemote(t *testing.T, args ...string) string {
	t.Helper()
	home, sessDir := tmpHomeSessions(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, buildBee(t), append([]string{"remote-control", "--port", "0"}, args...)...)
	cmd.Env = scriptedEnv(home, sessDir, absFixture(t, "simple_text.json"))
	out := &syncBuf{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	for !strings.Contains(out.String(), "press Ctrl+C") {
		if ctx.Err() != nil {
			t.Fatalf("remote-control never became ready:\n%s", out.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	return out.String()
}

// --yes is the LAN acknowledgement only; auto-approving dangerous commands
// from remote clients needs --yolo.
func TestRemoteControlYesDoesNotAutoApprove(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e under -short")
	}
	out := startRemote(t, "--yes")
	if strings.Contains(out, "auto-approved") {
		t.Errorf("--yes alone auto-approved dangerous commands:\n%s", out)
	}
	if !strings.Contains(out, "will auto-deny. pass --yolo") {
		t.Errorf("missing auto-deny notice naming --yolo:\n%s", out)
	}
}

func TestRemoteControlYoloAutoApproves(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e under -short")
	}
	out := startRemote(t, "--yes", "--yolo")
	if !strings.Contains(out, "dangerous commands are auto-approved") {
		t.Errorf("--yolo did not announce auto-approve:\n%s", out)
	}
}
