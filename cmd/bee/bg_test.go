package main

import (
	"testing"
)

func TestParseBgArgsEmpty(t *testing.T) {
	msg, opts, err := parseBgArgs(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != "" {
		t.Fatalf("expected empty message, got: %q", msg)
	}
	if opts.Skill != "" || opts.LogFile != "" {
		t.Fatalf("expected zero opts, got: %+v", opts)
	}
}

func TestParseBgArgsAcceptsMessage(t *testing.T) {
	msg, opts, err := parseBgArgs([]string{"hello", "world"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != "hello world" {
		t.Fatalf("message: got %q want %q", msg, "hello world")
	}
	if opts.Skill != "" || opts.LogFile != "" {
		t.Fatalf("opts should be zero, got %+v", opts)
	}
}

func TestParseBgArgsSkillOnly(t *testing.T) {
	msg, opts, err := parseBgArgs([]string{"--skill", "calc"})
	if err != nil {
		t.Fatalf("skill-only invocation should be allowed: %v", err)
	}
	if msg != "" {
		t.Fatalf("expected empty message, got %q", msg)
	}
	if opts.Skill != "calc" {
		t.Fatalf("skill: got %q want %q", opts.Skill, "calc")
	}
}

func TestParseBgArgsFlags(t *testing.T) {
	msg, opts, err := parseBgArgs([]string{"--skill", "calc", "--logfile", "/tmp/x.log", "do", "thing"})
	if err != nil {
		t.Fatal(err)
	}
	if msg != "do thing" {
		t.Fatalf("msg: %q", msg)
	}
	if opts.Skill != "calc" {
		t.Fatalf("skill: %q", opts.Skill)
	}
	if opts.LogFile != "/tmp/x.log" {
		t.Fatalf("logfile: %q", opts.LogFile)
	}
}

func TestParseBgArgsList(t *testing.T) {
	msg, opts, err := parseBgArgs([]string{"--list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != "" {
		t.Fatalf("expected empty message, got %q", msg)
	}
	if !opts.List {
		t.Fatal("expected List=true")
	}
}

func TestParseBgArgsTail(t *testing.T) {
	msg, opts, err := parseBgArgs([]string{"--tail", "abc123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != "" {
		t.Fatalf("expected empty message, got %q", msg)
	}
	if opts.Tail != "abc123" {
		t.Fatalf("tail: got %q want abc123", opts.Tail)
	}
}

func TestParseBgArgsKill(t *testing.T) {
	msg, opts, err := parseBgArgs([]string{"--kill", "xyz789"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != "" {
		t.Fatalf("expected empty message, got %q", msg)
	}
	if opts.Kill != "xyz789" {
		t.Fatalf("kill: got %q want xyz789", opts.Kill)
	}
}
