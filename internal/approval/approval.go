// Package approval gates dangerous shell commands behind a user decision.
//
// Pattern: safety.DetectDangerous flags a command -> Approver.Request asks the
// user how to proceed. Decisions cache for the session; AllowAlways persists
// via the caller-supplied callback (typically writes config.command_allowlist).
package approval

import (
	"context"
	"errors"
	"fmt"
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
// Lookup precedence: persistent → session → inner.
type Cache struct {
	inner       Approver
	mu          sync.Mutex
	session     map[string]bool
	persistent  map[string]bool
	persistFunc func(key string) error
	// persistWG tracks in-flight async persist goroutines so Flush can wait
	// for them on shutdown.
	persistWG sync.WaitGroup
	// persistMu serializes persistFunc: each call is a read-modify-write of
	// one file, so two grants must not interleave.
	persistMu    sync.Mutex
	persistErrs  []error
	onPersistErr func(key string, err error)
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

// OnPersistError registers fn to hear about a failed AllowAlways write. The
// grant still holds for the session; only the next run would ask again.
func (c *Cache) OnPersistError(fn func(key string, err error)) {
	c.mu.Lock()
	c.onPersistErr = fn
	c.mu.Unlock()
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
	}
	c.mu.Unlock()
	// persistFunc may block 50-200ms on disk IO; run async so the user gets
	// the verdict immediately.
	if pf := c.persistFunc; d == AllowAlways && pf != nil {
		c.persistWG.Add(1)
		go func() {
			defer c.persistWG.Done()
			c.persist(pf, key)
		}()
	}
	return d, nil
}

func (c *Cache) persist(pf func(string) error, key string) {
	c.persistMu.Lock()
	err := pf(key)
	c.persistMu.Unlock()
	if err == nil {
		return
	}
	err = fmt.Errorf("persist allowlist %q: %w", key, err)
	c.mu.Lock()
	c.persistErrs = append(c.persistErrs, err)
	fn := c.onPersistErr
	c.mu.Unlock()
	if fn != nil {
		fn(key, err)
	}
}

// Flush blocks until all in-flight persistFunc goroutines complete and
// returns every persist failure so far. Call from the shutdown path so
// config.toml writes aren't lost on exit.
func (c *Cache) Flush() error {
	if c == nil {
		return nil
	}
	c.persistWG.Wait()
	c.mu.Lock()
	defer c.mu.Unlock()
	return errors.Join(c.persistErrs...)
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
