package loop

import (
	"github.com/elhenro/bee/internal/herdr"
	"github.com/elhenro/bee/internal/session"
)

// herdrSession is the rollout to report to the hosting pane, zero for engines
// that are not the pane's host.
func (e *Engine) herdrSession() herdr.Session {
	if !e.HostSession || e.Sessions == nil || e.Sessions.ID() == "" {
		return herdr.Session{}
	}
	id := e.Sessions.ID()
	path, _ := session.Path(id)
	return herdr.Session{ID: id, Path: path}
}
