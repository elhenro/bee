package herdr

import "time"

// loop is the single sender goroutine: it wakes on every state change and
// pushes the latest desired report, so bursts coalesce into one call.
func (r *Reporter) loop() {
	defer close(r.done)
	for {
		select {
		case <-r.wake:
			r.sync()
		case <-r.quit:
			r.release()
			return
		}
	}
}

func (r *Reporter) sync() {
	for {
		want := r.desired()
		if r.sent != nil && want == *r.sent {
			return
		}
		if want.state == idle && r.sent != nil && r.sent.state != idle {
			select {
			case <-time.After(r.settle):
			case <-r.wake:
				continue
			case <-r.quit:
				return
			}
			// activity resumed during the hold: report that instead
			if r.desired() != want {
				continue
			}
		}
		r.deliver(want)
		return
	}
}

func (r *Reporter) deliver(want report) {
	params := r.base()
	params["state"] = string(want.state)
	if want.message != "" {
		params["message"] = want.message
	}
	if s := want.session; s.ID != "" {
		params["agent_session_id"] = s.ID
		if s.Path != "" {
			params["agent_session_path"] = s.Path
		}
		// herdr replays this after its own restart to bring the pane back
		params["resume_argv"] = []string{agent, "back", s.ID}
	}
	if err := r.tr.send("pane.report_agent", params); err != nil {
		return
	}
	r.sent = &want
}

func (r *Reporter) release() {
	if r.sent == nil {
		return
	}
	_ = r.tr.send("pane.release_agent", r.base())
}

func (r *Reporter) base() map[string]any {
	// herdr drops reports whose seq is not newer than the last one it saw
	seq := time.Now().UnixNano()
	if seq <= r.seq {
		seq = r.seq + 1
	}
	r.seq = seq
	return map[string]any{
		"pane_id": r.pane,
		"source":  source,
		"agent":   agent,
		"seq":     seq,
	}
}
