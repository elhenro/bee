package tui

import (
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) onLoaderTick(_ loaderTickMsg) (tea.Model, tea.Cmd) {
	// Only animate while a turn or async compact is in flight. Letting the
	// tick die when we leave streaming/compacting/judging keeps idle terminals quiet.
	if m.state != StateStreaming && !m.compacting && !m.goalJudging {
		return m, nil
	}
	// inactivity watchdog: a streaming turn that has shown activity and then
	// gone silent (no stream/think/tool/warn delta) for watchdogStall is
	// treated as stalled — cancel it and wait for the cancelled turn to land
	// before resubmitting (so two eng.Run calls never overlap). Held off until
	// the turn's first sign of life (turnSawActivity): a queued or prefilling
	// request on a busy local model is silent for a while, and resubmitting it
	// only deepens the server queue — a turn that never produces anything is
	// caught by the provider-level stall guard instead. Suppressed while the
	// user is mid-steer (typing) and once a stall-resume is already pending.
	// A running tool is silent by design (long build, batch job) and bounded by
	// its own timeout: cancelling it here kills the work and the resumed model
	// just re-runs the same call.
	if m.state == StateStreaming && m.watchdogEnabled && !m.watchdogDisabled &&
		!m.stallResumePending && strings.TrimSpace(m.input.Value()) == "" &&
		len(m.pendingTools) == 0 && m.turnSawActivity && time.Since(m.lastActivityAt) >= m.watchdogStall {
		// continue from partial output when the turn produced any, else
		// re-send the original instruction.
		text := resumeContinueText
		if m.turnOutChars == 0 {
			text = lastUserText(m.messages)
		}
		// cancel the hang even at the resume cap — retrigger parks it in
		// StateError ("gave up") rather than leaving the turn wedged.
		if strings.TrimSpace(text) != "" {
			if m.cancelRun != nil {
				m.cancelRun()
				m.cancelRun = nil
			}
			m.stallResumePending = true
			m.stallResumeText = text
			m.stallResumeReason = "stalled"
			m = m.withWarn("bee stalled — auto-resuming…")
			return m, tea.Batch(warningFadeCmd(), loaderTickCmd())
		}
	}
	m.loaderFrame++
	// sample output throughput once per tick — drives particle density. Clamp
	// to 0 so a partial reset on a tool boundary never yields a negative rate.
	d := max(m.turnOutChars-m.loaderSampleChars, 0)
	m.loaderRate = d
	// stall tracker for the busy-row dance: consecutive ticks with no output
	// growth. Any growth resets it, so the form flips back to the round
	// dance the moment tokens resume.
	if d > 0 {
		m.loaderStallFrames = 0
	} else {
		m.loaderStallFrames++
	}
	// EMA smooths the 1-tick flash when a provider flushes a large chunk
	// into a single tick (or when hasThinkingBlock resets the rate between
	// reasoning and answer). α=0.3 → ~3-tick rise time — fast enough to
	// still feel responsive, slow enough to never balloon the particle
	// count in a single frame.
	m.loaderRateEMA = smoothLoaderRate(m.loaderRateEMA, d)
	m.loaderSampleChars = m.turnOutChars
	// sliding 10s window for the tok/s readout. Push a sample only when
	// this tick actually produced chars, then drop anything older than 10s.
	// Idle gaps contribute no samples, so the window reflects only the last
	// 10s of active generation — no spikes, no slow EMA bleed.
	now := time.Now()
	if d > 0 {
		m.loaderRateSamples = append(m.loaderRateSamples, rateSample{at: now, chars: d})
	}
	cutoff := now.Add(-10 * time.Second)
	m.loaderRateSamples = slices.DeleteFunc(m.loaderRateSamples, func(s rateSample) bool { return !s.at.After(cutoff) })
	var winChars int
	for _, s := range m.loaderRateSamples {
		winChars += s.chars
	}
	m.loaderRateTokS = float64(winChars) / 10.0 / charsPerToken
	return m, loaderTickCmd()
}

// onGlowTick advances the rainbow input animation. Self-rearms only while the
// mastermind tier is active so switching it off lets the tick die and leaves
// idle terminals quiet.
func (m Model) onGlowTick(_ glowTickMsg) (tea.Model, tea.Cmd) {
	if m.role != "queen" {
		return m, nil
	}
	m.glowFrame++
	return m, glowTickCmd()
}

func (m Model) onCostTick(_ costTickMsg) (tea.Model, tea.Cmd) {
	if m.costFlashFrame >= m.costFlashUntil {
		m.costFlashUntil = 0
		return m, nil
	}
	m.costFlashFrame++
	return m, costTickCmd()
}

func (m Model) onIntroTick(_ introTickMsg) (tea.Model, tea.Cmd) {
	if m.introActive {
		// build frames on first tick when width is finally known. If width
		// still hasn't arrived (initial WindowSizeMsg pending), just rearm.
		if m.introFrames == nil {
			if m.width <= 0 {
				return m, introTickCmd()
			}
			m.introFrames = introFrames(m.introStyle, m.width)
		}
		m.introIdx++
		if m.introIdx >= len(m.introFrames) {
			m.introActive = false
			m.introFrames = nil
			m.introDone = true
			m.introDoneFrame = 0
		}
		return m, introTickCmd()
	}
	// post-intro pulse: keep ticking until the bold-flash cycle settles,
	// then push the banner to terminal scrollback so it stays anchored at
	// the top of the conversation. Clearing introDone collapses the live
	// region; the scrollback push adds the full placeholder block above, so
	// the input bar stays vertically stable.
	if m.introDone && m.introDoneFrame < introPulseFrames {
		m.introDoneFrame++
		if m.introDoneFrame < introPulseFrames {
			return m, introTickCmd()
		}
		banner := renderIntroPlaceholder(m.width, introPulseFrames)
		m.introDone = false
		// intro anchored — surface the deferred first-run welcome gate now.
		if m.tutorialPending {
			m.tutorialPending = false
			m.tutorial = tutorialState{active: true, phase: tutPhaseGate}
		}
		return m, m.scroll.print(banner)
	}
	return m, nil
}
