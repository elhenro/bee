package main

import (
	"strings"
	"testing"
)

func TestParseHyperplanArgsRejectsEmpty(t *testing.T) {
	_, _, err := parseHyperplanArgs(nil)
	if err == nil {
		t.Fatal("expected error for empty args")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected 'missing' in error, got: %v", err)
	}
}

func TestParseHyperplanArgsAcceptsMessage(t *testing.T) {
	msg, opts, err := parseHyperplanArgs([]string{"add", "OAuth"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != "add OAuth" {
		t.Fatalf("message: got %q want %q", msg, "add OAuth")
	}
	if opts.N != 5 {
		t.Fatalf("default N: got %d want 5", opts.N)
	}
	if opts.Model != "" || opts.Provider != "" {
		t.Fatalf("opts should default to empty overrides, got %+v", opts)
	}
}

func TestParseHyperplanArgsOverrideN(t *testing.T) {
	msg, opts, err := parseHyperplanArgs([]string{"--n", "3", "do", "thing"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != "do thing" {
		t.Fatalf("msg: %q", msg)
	}
	if opts.N != 3 {
		t.Fatalf("N: got %d want 3", opts.N)
	}
}

func TestParseHyperplanArgsRejectsNegativeN(t *testing.T) {
	_, _, err := parseHyperplanArgs([]string{"--n", "0", "x"})
	if err == nil {
		t.Fatal("expected error for --n 0")
	}
}

func TestParseHyperplanArgsModelProviderOverride(t *testing.T) {
	msg, opts, err := parseHyperplanArgs([]string{"--model", "gpt-4o", "--provider", "openai", "plan", "it"})
	if err != nil {
		t.Fatal(err)
	}
	if msg != "plan it" {
		t.Fatalf("msg: %q", msg)
	}
	if opts.Model != "gpt-4o" {
		t.Fatalf("model: %q", opts.Model)
	}
	if opts.Provider != "openai" {
		t.Fatalf("provider: %q", opts.Provider)
	}
}

func TestCriticPromptWedgesAngle(t *testing.T) {
	got := criticPrompt("ship feature X", 0)
	if !strings.Contains(got, "ship feature X") {
		t.Errorf("missing plan body in prompt:\n%s", got)
	}
	if !strings.Contains(got, "security") {
		t.Errorf("expected security angle for idx 0:\n%s", got)
	}
	if !strings.Contains(got, "do NOT propose fixes") {
		t.Errorf("missing critic instruction:\n%s", got)
	}
}

func TestCriticPromptCyclesAngles(t *testing.T) {
	// idx 5 wraps back to security (5 % 5 = 0)
	got := criticPrompt("plan", 5)
	if !strings.Contains(got, "security") {
		t.Errorf("expected angle cycling at idx 5:\n%s", got)
	}
}
