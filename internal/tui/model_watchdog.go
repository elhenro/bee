package tui

import "time"

// watchdogState is the stall-detection and auto-resume state, embedded in Model.
type watchdogState struct {
	// watchdog: inactivity stall detection + bounded auto-resume so a turn
	// that hangs or stops on a recoverable error finishes unattended. Mirrored
	// from cfg.Watchdog (see seedFromEngine). The stall check reads
	// time.Since(lastActivityAt) — NOT turn duration — so a productive long
	// turn (output still streaming) is never interrupted.
	watchdogEnabled    bool
	watchdogStall      time.Duration
	watchdogMaxResumes int
	watchdogDisabled   bool      // /watchdog off — session-only opt-out
	lastActivityAt     time.Time // bumped on every stream/think/live/warn delta
	turnSawActivity    bool      // first-token grace: false until the turn shows life; gates the stall watchdog
	resumeCount        int       // consecutive auto-resumes for the current task
	awaitingProgress   bool      // set after a resume; first activity resets resumeCount
	resumeErrGen       int       // invalidates a scheduled error-resume on a newer turn
	// stall-resume handshake: on a stall we cancel the run and wait for its
	// context.Canceled turnDoneMsg before resubmitting, so two eng.Run calls
	// never overlap on the shared engine.
	stallResumePending bool
	stallResumeText    string
	stallResumeReason  string
}
