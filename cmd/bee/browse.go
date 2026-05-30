package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// browseArgsToRun turns `bee browse <url> [instructions...]` into args for
// the headless run path: enable the browser and seed an open/observe prompt.
func browseArgsToRun(args []string) ([]string, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return nil, errors.New("usage: bee browse <url> [instructions]")
	}
	url := args[0]
	extra := strings.TrimSpace(strings.Join(args[1:], " "))
	prompt := fmt.Sprintf(
		"Open %s with browser_open, read the accessibility snapshot, then call browser_console and report any errors. %s",
		url, extra,
	)
	return []string{"--browser", "--headless=false", prompt}, nil
}

// browse is the `bee browse` subcommand entry point.
func browse(args []string) {
	runArgs, err := browseArgsToRun(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	runHeadless(runArgs)
}
