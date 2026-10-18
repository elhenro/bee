package main

import (
	"context"
	"testing"

	"github.com/elhenro/bee/internal/config"
)

func TestTurnContext(t *testing.T) {
	cases := []struct {
		name         string
		w            config.WatchdogConfig
		wantDeadline bool
	}{
		{"enabled default", config.WatchdogConfig{Enabled: true}, true},
		{"disabled no timeout", config.WatchdogConfig{}, false},
		{"disabled explicit timeout", config.WatchdogConfig{TimeoutSec: 30}, true},
	}
	for _, tc := range cases {
		ctx, cancel := turnContext(context.Background(), tc.w)
		_, has := ctx.Deadline()
		cancel()
		if has != tc.wantDeadline {
			t.Errorf("%s: deadline=%v want %v", tc.name, has, tc.wantDeadline)
		}
	}
}
