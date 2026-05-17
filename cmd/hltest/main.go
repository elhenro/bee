package main

import (
	"fmt"
	"github.com/elhenro/bee/internal/tui"
)

func main() {
	for _, s := range []string{
		"\tdone := make(chan struct{})",
		"\tshellBin, finalCmd := t.buildInvocation(cmdStr)",
		"done := make(chan struct{})",
		"shellBin, finalCmd := t.buildInvocation(cmdStr)",
	} {
		out := tui.HighlightCode(s, "go")
		fmt.Printf("IN:%q\nOUT:%q\n\n", s, out)
	}
}
