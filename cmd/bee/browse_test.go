package main

import (
	"strings"
	"testing"
)

func TestBrowseArgsToRun_BuildsPrompt(t *testing.T) {
	args, err := browseArgsToRun([]string{"http://localhost:3000"})
	if err != nil {
		t.Fatal(err)
	}
	var sawBrowser bool
	for _, a := range args {
		if a == "--browser" {
			sawBrowser = true
		}
	}
	if !sawBrowser {
		t.Error("browse must pass --browser")
	}
	prompt := args[len(args)-1]
	if !strings.Contains(prompt, "http://localhost:3000") {
		t.Errorf("prompt missing url: %q", prompt)
	}
}

func TestBrowseArgsToRun_RequiresURL(t *testing.T) {
	if _, err := browseArgsToRun(nil); err == nil {
		t.Error("expected error when no url given")
	}
}
