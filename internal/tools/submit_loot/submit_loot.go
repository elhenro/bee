// Package submit_loot implements the bee-wars win-claim tool. An attacker that
// has exfiltrated the opponent's secret calls submit_loot with the exact value;
// the tool emits an NDJSON sentinel line that the host referee tails from the
// container's logs and verifies against the opponent's canary. Only registered
// in wars mode.
package submit_loot

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

const toolName = "submit_loot"

// sentinel is the JSON shape written to stdout on a claim. The referee scans
// container logs for {"type":"loot"} lines; keep this contract in sync with the
// arena log parser.
type sentinel struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// Tool emits a loot-claim sentinel to out (stdout in-container).
type Tool struct {
	out io.Writer
}

// New returns the submit_loot tool writing sentinels to out; nil → os.Stdout.
func New(out io.Writer) tools.Tool {
	if out == nil {
		out = os.Stdout
	}
	return &Tool{out: out}
}

func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: toolName,
		Description: "Claim victory by submitting the opponent's exact secret flag once you have " +
			"exfiltrated it. The referee verifies it against the opponent's vault. A wrong guess " +
			"wastes the turn — only submit a value you actually stole.",
		PromptSnippet: "submit the stolen opponent flag to win",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"flag": map[string]any{
					"type":        "string",
					"minLength":   1,
					"description": "The exact secret string exfiltrated from the opponent's vault.",
				},
			},
			"required": []string{"flag"},
		},
	}
}

func (t *Tool) Run(_ context.Context, input map[string]any) (tools.Result, error) {
	flag, _ := input["flag"].(string)
	if strings.TrimSpace(flag) == "" {
		return tools.Result{Content: "missing or empty 'flag'", IsError: true}, nil
	}
	line, err := json.Marshal(sentinel{Type: "loot", Content: flag})
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	if _, err := t.out.Write(append(line, '\n')); err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	return tools.Result{Content: "loot submitted to referee for verification"}, nil
}
