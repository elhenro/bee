package arena

import (
	"context"
	"fmt"
	"time"
)

// NewMatch wires a match from two combatants, a runtime, a built image, the
// referee proxy + its container-reachable URL, and the config. Wallets and ELO
// are expected to be seeded by the caller (the CLI applies the handicap).
func NewMatch(id string, red, blue *Combatant, rt Runtime, proxy *MeteringProxy, proxyURL, image string, cfg Config) *Match {
	return &Match{
		ID: id, Red: red, Blue: blue, Cfg: cfg,
		Net: "bee-combat-" + id, Image: image, ProxyURL: proxyURL,
		rt: rt, proxy: proxy,
	}
}

// spawn runs one combatant container on the no-egress combat net, then injects
// its per-match secret into the tmpfs vault (never an image layer).
func (m *Match) spawn(ctx context.Context, c, opp *Combatant) error {
	c.Container = fmt.Sprintf("%s-%s", m.ID, c.Name)
	spec := ContainerSpec{
		Name:    c.Container,
		Image:   m.Image,
		Network: m.Net,
		Alias:   c.Name,
		Env: map[string]string{
			"BEE_WARS_SIDE":     c.Name,
			"BEE_WARS_OPPONENT": fmt.Sprintf("http://%s:8080", opp.Name),
			"BEE_WARS_VULN":     m.Cfg.Vuln,
			// every model call flows through the referee proxy: metered + routed
			"OPENAI_BASE_URL": fmt.Sprintf("%s/%s/v1", m.ProxyURL, c.Name),
			"BEE_MODEL":       c.Model,
		},
		Tmpfs:     []string{"/opt/vault:size=64k", "/tmp:size=16m"},
		Cmd:       []string{"wars-agent"},
		ReadOnly:  true,
		MemoryMB:  1024,
		PidsLimit: 256,
		Cpus:      "1",
	}
	if _, err := m.rt.Run(ctx, spec); err != nil {
		return err
	}
	// inject the secret out-of-band so it never touches an image layer
	inject := fmt.Sprintf("printf %%s %q > /opt/vault/secret.txt", c.Secret)
	if _, err := m.rt.Exec(ctx, c.Container, "sh", "-c", inject); err != nil {
		return fmt.Errorf("inject secret: %w", err)
	}
	return nil
}

// teardown removes both containers and the network, best-effort, on a fresh
// context so it still runs after the match context is cancelled.
func (m *Match) teardown() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, c := range []*Combatant{m.Red, m.Blue} {
		if c.Container != "" {
			_ = m.rt.Remove(ctx, c.Container)
		}
	}
	_ = m.rt.NetworkRemove(ctx, m.Net)
}
