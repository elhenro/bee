package loop

import (
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func TestEmitSteerDelivered(t *testing.T) {
	ch := make(chan types.Message, 1)
	e := &Engine{LiveMsgCh: ch}
	e.emitSteerDelivered("use postgres")
	m := <-ch
	if !m.Ephemeral || !strings.Contains(m.Content[0].Text, "steer delivered: use postgres") {
		t.Fatalf("bad notice: %+v", m)
	}
}
