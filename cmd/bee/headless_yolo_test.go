package main

import (
	"testing"

	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/config"
)

// TestBuildHeadlessApprover_Yolo verifies the headless approver auto-approves
// when either the --yolo/--yes flag (autoYes) or the persisted cfg.Yolo toggle
// is set — and reads cfg.Yolo, not the removed cfg.Mode.
func TestBuildHeadlessApprover_Yolo(t *testing.T) {
	cases := []struct {
		name      string
		cfg       config.Config
		autoYes   bool
		wantStatic bool
	}{
		{"flag", config.Config{}, true, true},
		{"cfg toggle", config.Config{Yolo: true}, false, true},
		{"neither", config.Defaults(), false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ap := buildHeadlessApprover(tc.cfg, tc.autoYes)
			_, isStatic := ap.(approval.Static)
			if isStatic != tc.wantStatic {
				t.Errorf("static approver = %v, want %v", isStatic, tc.wantStatic)
			}
		})
	}
}
