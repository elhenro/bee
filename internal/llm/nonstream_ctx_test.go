package llm

import (
	"context"
	"testing"
	"time"
)

// a one-shot loop must give up on a cancelled ctx instead of blocking on a
// consumer that stopped reading.
func TestNonStreamLoopsStopOnCancel(t *testing.T) {
	loops := map[string]struct {
		run  respLoop
		body string
	}{
		"claude":  {(&ClaudeProvider{}).nonStreamLoop, `{"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn"}`},
		"compat":  {(&OpenAICompatProvider{}).nonStreamLoop, `{"choices":[{"message":{"content":"hi"},"finish_reason":"stop"}]}`},
		"chatgpt": {(&ChatGPTProvider{}).nonStreamLoop, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hi"}]}]}`},
	}
	for name, l := range loops {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			out := make(chan Event) // unbuffered, nobody reading
			done := make(chan struct{})
			go func() {
				l.run(ctx, sseResp(l.body), out)
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("loop blocked on send after ctx cancel")
			}
		})
	}
}
