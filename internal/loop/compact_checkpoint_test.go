package loop

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/types"
)

func idMsg(id string, role types.Role, text string) types.Message {
	m := mkMsg(role, text)
	m.ID = id
	return m
}

func TestCompactionModel_PrefersFast(t *testing.T) {
	if got := compactionModel(config.Config{DefaultModel: "big", FastModel: "small"}); got != "small" {
		t.Errorf("want small, got %q", got)
	}
	if got := compactionModel(config.Config{DefaultModel: "big"}); got != "big" {
		t.Errorf("empty FastModel should fall back to default, got %q", got)
	}
}

func TestCompact_PersistsCheckpoint(t *testing.T) {
	t.Setenv("BEE_SESSIONS_DIR", t.TempDir())
	sid := uuid.NewString()
	roll, err := session.Open(sid)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { roll.Close() })

	msgs := []types.Message{
		idMsg("m1", types.RoleUser, "1"),
		idMsg("m2", types.RoleAssistant, "2"),
		idMsg("m3", types.RoleUser, "3"),
		idMsg("m4", types.RoleAssistant, "4"),
		idMsg("m5", types.RoleUser, "5"),
		idMsg("m6", types.RoleAssistant, "6"),
	}
	e := &Engine{
		Provider: &compactStubProvider{summary: "SUMMARY"},
		Sessions: roll,
		Cfg:      config.Config{DefaultModel: "stub", Compaction: config.CompactionConfig{Enabled: true}},
	}
	out, stats, err := e.compact(context.Background(), msgs)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if stats.AfterMsgs >= stats.BeforeMsgs {
		t.Fatalf("expected compaction to shrink: before=%d after=%d", stats.BeforeMsgs, stats.AfterMsgs)
	}
	// preserved tail starts at m3 (len 6 - PreserveTail 4).
	if out[1].ID != "m3" {
		t.Fatalf("preserve boundary want m3, got %q", out[1].ID)
	}
	// a checkpoint marker should now sit on disk pointing at m3.
	raw, err := session.Read(sid)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var cp *types.Message
	for i := range raw {
		if raw[i].Checkpoint != nil {
			cp = &raw[i]
		}
	}
	if cp == nil {
		t.Fatal("no checkpoint persisted")
	}
	if cp.Checkpoint.PreserveFrom != "m3" {
		t.Errorf("checkpoint PreserveFrom want m3, got %q", cp.Checkpoint.PreserveFrom)
	}
}

func TestPrepareResume_CompactsOversizedSeed(t *testing.T) {
	t.Setenv("BEE_SESSIONS_DIR", t.TempDir())
	sid := uuid.NewString()
	roll, err := session.Open(sid)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { roll.Close() })

	// big history: ~40k estimated tokens (len/4) clears the scaled threshold on
	// the 32k local-provider budget floor.
	big := make([]types.Message, 0, 10)
	for i := 0; i < 10; i++ {
		big = append(big, idMsg(uuid.NewString(), types.RoleUser, string(make([]byte, 16000))))
	}
	e := &Engine{
		Provider:        &compactStubProvider{summary: "SUMMARY"},
		Sessions:        roll,
		Cfg:             config.Config{DefaultProvider: "ollama", DefaultModel: "stub", Compaction: config.CompactionConfig{Enabled: true, Threshold: 0.75}},
		InitialMessages: big,
	}
	_, ran, err := e.PrepareResume(context.Background())
	if err != nil {
		t.Fatalf("PrepareResume: %v", err)
	}
	if !ran {
		t.Fatal("expected compaction to run on oversized seed")
	}
	if len(e.InitialMessages) >= len(big) {
		t.Fatalf("InitialMessages should shrink: %d -> %d", len(big), len(e.InitialMessages))
	}
}

func TestPrepareResume_SkipsSmallSeed(t *testing.T) {
	e := &Engine{
		Provider:        &compactStubProvider{summary: "X"},
		Cfg:             config.Config{DefaultProvider: "ollama", DefaultModel: "stub", Compaction: config.CompactionConfig{Enabled: true, Threshold: 0.75}},
		InitialMessages: []types.Message{idMsg("m1", types.RoleUser, "hi")},
	}
	_, ran, err := e.PrepareResume(context.Background())
	if err != nil {
		t.Fatalf("PrepareResume: %v", err)
	}
	if ran {
		t.Fatal("small seed should not trigger compaction")
	}
}
