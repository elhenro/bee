// Package approval gates dangerous shell commands behind a user decision.
//
// Pattern: safety.DetectDangerous flags a command -> Approver.Request asks the
// user how to proceed. Decisions cache for the session; AllowAlways persists
// via the caller-supplied callback (typically writes config.command_allowlist).
package approval

import (
	"context"
	"sync"
)

// Decision is the user's choice when prompted about a dangerous command.
type Decision int

const (
	// Deny: refuse this invocation. Model must try a different approach.
	Deny Decision = iota
	// AllowOnce: run this single invocation, do not cache.
	AllowOnce
	// AllowSession: run + cache the pattern key for the rest of the session.
	AllowSession
	// AllowAlways: run + persist the pattern key in the user's config.
	AllowAlways
)

// Approver decides whether a flagged command may run. Implementations vary by
// surface: TUI shows a modal, headless reads stdin or honours --yes/--yolo.
type Approver interface {
	Request(ctx context.Context, cmd, key, desc string) (Decision, error)
}

// Cache wraps an inner Approver with session memory + a persistent allowlist.
// Lookup precedence: persistent -> session -> inner.Request.
type Cache struct {
	inner       Approver
	mu          sync.Mutex
	session     map[string]bool
	persistent  map[string]bool
	persistFunc func(key string) error
}

// NewCache builds a Cache around inner. seed contains pattern keys already in
// the user's persistent allowlist. persist is invoked whenever a new key is
// granted AllowAlways; pass nil to disable persistence.
func NewCache(inner Approver, seed []string, persist func(key string) error) *Cache {
	c := &Cache{
		inner:       inner,
		session:     map[string]bool{},
		persistent:  map[string]bool{},
		persistFunc: persist,
	}
	for _, k := range seed {
		c.persistent[k] = true
	}
	return c
}

// Request consults the caches first, then defers to the inner Approver.
// AllowOnce never caches. AllowSession caches in-memory. AllowAlways caches +
// fires the persistFunc.
func (c *Cache) Request(ctx context.Context, cmd, key, desc string) (Decision, error) {
	c.mu.Lock()
	if c.persistent[key] {
		c.mu.Unlock()
		return AllowAlways, nil
	}
	if c.session[key] {
		c.mu.Unlock()
		return AllowSession, nil
	}
	c.mu.Unlock()

	d, err := c.inner.Request(ctx, cmd, key, desc)
	if err != nil {
		return Deny, err
	}
	c.mu.Lock()
	switch d {
	case AllowSession:
		c.session[key] = true
	case AllowAlways:
		c.session[key] = true
		c.persistent[key] = true
		// persistFunc may block on disk IO; release lock first.
		pf := c.persistFunc
		c.mu.Unlock()
		if pf != nil {
			if perr := pf(key); perr != nil {
				return d, perr
			}
		}
		return d, nil
	}
	c.mu.Unlock()
	return d, nil
}

// AlwaysAllowKeys returns the union of session + persistent grants. Useful for
// tests and for showing the user what they've trusted.
func (c *Cache) AlwaysAllowKeys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.persistent))
	for k := range c.persistent {
		out = append(out, k)
	}
	return out
}

// Static is a fixed-verdict Approver for headless / test use. Always returns
// the configured Decision without prompting.
type Static struct {
	Verdict Decision
}

// Request returns s.Verdict unchanged.
func (s Static) Request(_ context.Context, _ string, _ string, _ string) (Decision, error) {
	return s.Verdict, nil
}
