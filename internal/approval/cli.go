package approval

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
)

// CLI is a stdin/stderr Approver for the headless surface. Prints the prompt
// to err, reads a single line from in, maps the first char to a Decision.
// Concurrent calls are serialised so prompts don't interleave.
type CLI struct {
	in     io.Reader
	out    io.Writer
	mu     sync.Mutex
	reader *bufio.Reader
}

// NewCLI builds a CLI approver. Pass os.Stdin + os.Stderr for the normal case;
// override for tests.
func NewCLI(in io.Reader, out io.Writer) *CLI {
	return &CLI{in: in, out: out, reader: bufio.NewReader(in)}
}

// Request prints a prompt and reads the user's choice.
//
// Accepted answers (case-insensitive, first char):
//
//	a / y → AllowOnce
//	s     → AllowSession
//	f     → AllowAlways
//	d / n → Deny (also the default on empty input or EOF)
func (c *CLI) Request(ctx context.Context, cmd, key, desc string) (Decision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	fmt.Fprintf(c.out, "\n[approval] %s (%s)\n  command: %s\n  [a]llow once / [s]ession / [f]orever / [d]eny: ", desc, key, truncCmd(cmd))

	type result struct {
		d   Decision
		err error
	}
	done := make(chan result, 1)
	go func() {
		line, err := c.reader.ReadString('\n')
		if err != nil && line == "" {
			done <- result{Deny, nil} // EOF / closed stdin -> deny silently
			return
		}
		done <- result{parseAnswer(line), nil}
	}()

	select {
	case <-ctx.Done():
		return Deny, ctx.Err()
	case r := <-done:
		return r.d, r.err
	}
}

func parseAnswer(s string) Decision {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return Deny
	}
	switch s[0] {
	case 'a', 'y':
		return AllowOnce
	case 's':
		return AllowSession
	case 'f':
		return AllowAlways
	default:
		return Deny
	}
}

func truncCmd(cmd string) string {
	const max = 200
	if len(cmd) <= max {
		return cmd
	}
	return cmd[:max] + "..."
}
