package arena

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// ParseLoot scans one log line for the submit_loot sentinel and returns the
// claimed flag. Mirrors the contract in internal/tools/submit_loot.
func ParseLoot(line string) (string, bool) {
	i := strings.Index(line, `{"type":"loot"`)
	if i < 0 {
		return "", false
	}
	// decode exactly one JSON value starting at the sentinel; json.Decoder
	// respects braces inside strings (flags look like FLAG{...}) and tolerates
	// trailing log text after the object.
	var s struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	dec := json.NewDecoder(strings.NewReader(line[i:]))
	if err := dec.Decode(&s); err != nil || s.Type != "loot" {
		return "", false
	}
	return s.Content, true
}

// HandicapStart grants the lower-rated side extra starting nectar, scaled by the
// ELO gap and capped at +50%. The higher-rated side keeps the base. Equal
// ratings are even.
func HandicapStart(base int, eloA, eloB float64) (startA, startB int) {
	bonus := func(self, other float64) int {
		if self >= other {
			return base // not the weaker side
		}
		frac := (other - self) / 400.0
		if frac > 0.5 {
			frac = 0.5
		}
		return base + int(float64(base)*frac)
	}
	return bonus(eloA, eloB), bonus(eloB, eloA)
}

// Combatant is one side of a match: its model, the secret it guards (which the
// opponent must steal), its wallet, container, and rating.
type Combatant struct {
	Name       string
	Model      string
	Wallet     *Wallet
	Secret     string
	SecretHash string
	Container  string
	Session    string
	Elo        float64
}

// Match orchestrates one bee-wars bout end to end.
type Match struct {
	ID    string
	Red   *Combatant
	Blue  *Combatant
	Cfg      Config
	Net      string
	Image    string
	ProxyURL string // referee model proxy base, e.g. http://host.docker.internal:8800
	rt       Runtime
	proxy    *MeteringProxy
	Log      io.Writer
}

// claim is a verified-or-not loot submission observed in a container's logs.
type claim struct {
	by   string // submitting side
	flag string
}

// Run plays the match: build the no-egress network, assert containment, start
// both combatants, watch their logs for a verified capture, and settle. The
// caller is responsible for having built the image and proxy. Teardown is
// deferred so a cancel always cleans up.
func (m *Match) Run(ctx context.Context) (MatchResult, error) {
	if err := m.rt.NetworkCreate(ctx, m.Net, m.Cfg.Sealed); err != nil {
		return MatchResult{}, fmt.Errorf("network: %w", err)
	}
	defer m.teardown()

	for _, c := range []*Combatant{m.Red, m.Blue} {
		if err := m.spawn(ctx, c, m.opponent(c)); err != nil {
			return MatchResult{}, fmt.Errorf("spawn %s: %w", c.Name, err)
		}
		// only a sealed (--internal) net guarantees no egress; assert it.
		if m.Cfg.Sealed {
			if err := AssertNoEgress(ctx, m.rt, c.Container); err != nil {
				return MatchResult{}, err
			}
		}
	}

	claims := make(chan claim, 4)
	for _, c := range []*Combatant{m.Red, m.Blue} {
		go m.watch(ctx, c, claims)
	}

	deadline := time.NewTimer(m.matchTimeout())
	defer deadline.Stop()
	poll := time.NewTicker(2 * time.Second)
	defer poll.Stop()

	for {
		select {
		case <-ctx.Done():
			return m.settle("draw", "cancelled"), ctx.Err()
		case <-deadline.C:
			return m.settleByBalance(), nil
		case <-poll.C:
			if m.Red.Wallet.Bankrupt() {
				return m.settle(m.Blue.Name, "opponent_bankrupt"), nil
			}
			if m.Blue.Wallet.Bankrupt() {
				return m.settle(m.Red.Name, "opponent_bankrupt"), nil
			}
		case cl := <-claims:
			if m.verify(cl) {
				return m.settle(cl.by, "exfiltration"), nil
			}
		}
	}
}

// verify confirms a claimed flag matches the OPPONENT's secret (constant-time).
func (m *Match) verify(cl claim) bool {
	target := m.opponentByName(cl.by)
	if target == nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(cl.flag)), []byte(target.Secret)) == 1
}

// watch tails a combatant's container logs and forwards loot claims.
func (m *Match) watch(ctx context.Context, c *Combatant, out chan<- claim) {
	rc, err := m.rt.LogsStream(ctx, c.Container)
	if err != nil {
		return
	}
	defer rc.Close()
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		if flag, ok := ParseLoot(sc.Text()); ok {
			select {
			case out <- claim{by: c.Name, flag: flag}:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (m *Match) opponent(c *Combatant) *Combatant {
	if c == m.Red {
		return m.Blue
	}
	return m.Red
}

func (m *Match) opponentByName(name string) *Combatant {
	switch name {
	case m.Red.Name:
		return m.Blue
	case m.Blue.Name:
		return m.Red
	}
	return nil
}

func (m *Match) matchTimeout() time.Duration {
	// a generous wall-clock cap; the economy (bankruptcy) is the real limiter
	return time.Duration(m.Cfg.Rounds) * 2 * time.Minute
}

// settle records the outcome, applies the capture transfer + ELO update, and
// returns the ledger row.
func (m *Match) settle(winner, reason string) MatchResult {
	if winner != "draw" && reason == "exfiltration" {
		loser := m.opponentByName(winner)
		win := m.byName(winner)
		share := int(float64(loser.Wallet.Balance) * m.Cfg.CaptureShare)
		Transfer(loser.Wallet, win.Wallet, share)
	}
	scoreRed := EloScore(winner, m.Red.Name)
	newRed, newBlue := Elo(m.Red.Elo, m.Blue.Elo, scoreRed, 24)
	m.Red.Elo, m.Blue.Elo = newRed, newBlue

	return MatchResult{
		Time: time.Now().UTC().Format(time.RFC3339), MatchID: m.ID,
		Winner: winner, Reason: reason, Rounds: m.Cfg.Rounds,
		RedModel: m.Red.Model, BlueModel: m.Blue.Model, StartTokens: m.Cfg.StartTokens,
		RedBalance: m.Red.Wallet.Balance, BlueBalance: m.Blue.Wallet.Balance,
		RedSpent: m.Red.Wallet.Spent, BlueSpent: m.Blue.Wallet.Spent,
		RedSession: m.Red.Session, BlueSession: m.Blue.Session,
		SecretHash: m.Red.SecretHash, Economy: m.Cfg.Economy,
	}
}

// settleByBalance decides a timed-out match on remaining nectar.
func (m *Match) settleByBalance() MatchResult {
	switch {
	case m.Red.Wallet.Balance > m.Blue.Wallet.Balance:
		return m.settle(m.Red.Name, "round_cap")
	case m.Blue.Wallet.Balance > m.Red.Wallet.Balance:
		return m.settle(m.Blue.Name, "round_cap")
	default:
		return m.settle("draw", "round_cap")
	}
}

func (m *Match) byName(name string) *Combatant {
	if name == m.Red.Name {
		return m.Red
	}
	return m.Blue
}
