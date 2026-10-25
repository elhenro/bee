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
	out    io.Writer
	mu     sync.Mutex
	reader *bufio.Reader
	// pending is a line read still in flight from a prompt whose ctx ended.
	// the next prompt waits on it instead of starting a second reader, so the
	// answer typed at the newer prompt is not swallowed by the stale one.
	pending chan cliResult
}

type cliResult struct {
	d   Decision
	err error
}

// NewCLI builds a CLI approver. Pass os.Stdin + os.Stderr for the normal case;
// override for tests.
func NewCLI(in io.Reader, out io.Writer) *CLI {
	return &CLI{out: out, reader: bufio.NewReader(in)}
}

// Request prints a prompt and reads the user's choice.
//
// Accepted answers (case-insensitive, whole word; anything else denies):
//
//	a / y / yes / allow / once   → AllowOnce
//	s / session                  → AllowSession
//	f / always / forever         → AllowAlways
//	d / n / no / deny / cancel   → Deny (also the default on empty input or EOF)
func (c *CLI) Request(ctx context.Context, cmd, key, desc string) (Decision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	fmt.Fprintf(c.out, "\n[approval] %s (%s)\n  command: %s\n  [a]llow once / [s]ession / [f]orever / [d]eny: ", desc, key, showCmd(cmd))

	if c.pending == nil {
		done := make(chan cliResult, 1)
		c.pending = done
		go func() {
			line, err := c.reader.ReadString('\n')
			if err != nil && line == "" {
				done <- cliResult{Deny, nil} // EOF / closed stdin -> deny silently
				return
			}
			done <- cliResult{parseAnswer(line), nil}
		}()
	}

	select {
	case <-ctx.Done():
		return Deny, ctx.Err()
	case r := <-c.pending:
		c.pending = nil
		return r.d, r.err
	}
}

func parseAnswer(s string) Decision {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "a", "y", "yes", "allow", "once":
		return AllowOnce
	case "s", "session":
		return AllowSession
	case "f", "always", "forever":
		return AllowAlways
	default:
		return Deny
	}
}

// showCmd renders the command for the prompt: control characters escaped so a
// crafted command cannot redraw the terminal (ESC, CR, backspace) or hide
// text, and long commands keep both ends because the tail often carries the
// dangerous part.
func showCmd(cmd string) string {
	const keep = 200
	var b strings.Builder
	for _, r := range cmd {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) <= 2*keep {
		return out
	}
	head, tail := out[:keep], out[len(out)-keep:]
	return fmt.Sprintf("%s ... [%d bytes hidden] ... %s", strings.ToValidUTF8(head, ""), len(out)-2*keep, strings.ToValidUTF8(tail, ""))
}
