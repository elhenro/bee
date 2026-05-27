package loop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// ToolCallSignature identifies a tool call by name + canonical-args hash.
// Two calls with semantically-equal args (key order, whitespace in JSON
// stringification) collide on the same Sig.
type ToolCallSignature struct {
	Name     string
	ArgsHash string
}

// Observation summarizes what the tracker learned about a freshly observed
// (ToolUse, isError) pair. Caller decides how to react.
type Observation struct {
	Sig                         ToolCallSignature
	RepeatCount                 int  // how many times this exact Sig has fired this Run
	ConsecutiveSameToolFailures int  // streak of failures on the same tool name (any args)
	ConsecutiveSameSigFailures  int  // streak of failures on the same (tool,args) sig
	IsTwoStrike                 bool // same Sig failed twice in a row → nudge (no longer bails)
}

// repeatTracker is a per-Run signal collector. cheap to allocate.
type repeatTracker struct {
	// counts every signature seen this Run (success or fail).
	sigCounts map[ToolCallSignature]int
	// fail-streak by tool name; resets when that tool succeeds.
	failByTool map[string]int
	// fail-streak by (tool,args) sig; resets on success or different sig.
	failBySig int
	// last signature seen and whether it failed. used for two-strike detect.
	lastSig    ToolCallSignature
	lastFailed bool
	hasLast    bool
}

func newRepeatTracker() *repeatTracker {
	return &repeatTracker{
		sigCounts:  map[ToolCallSignature]int{},
		failByTool: map[string]int{},
	}
}

// Observe records one tool call and returns aggregates. Safe to call with
// any ToolUse; never errors.
func (t *repeatTracker) Observe(u types.ToolUse, isErr bool) Observation {
	sig := signatureFor(u)
	t.sigCounts[sig]++
	if isErr {
		t.failByTool[u.Name]++
	} else {
		t.failByTool[u.Name] = 0
	}
	twoStrike := false
	if t.hasLast && t.lastFailed && isErr && t.lastSig == sig {
		twoStrike = true
	}
	// failBySig: extend streak only when current is err AND sig matches last err sig.
	if isErr && t.hasLast && t.lastFailed && t.lastSig == sig {
		t.failBySig++
	} else if isErr {
		t.failBySig = 1
	} else {
		t.failBySig = 0
	}
	t.lastSig = sig
	t.lastFailed = isErr
	t.hasLast = true
	return Observation{
		Sig:                         sig,
		RepeatCount:                 t.sigCounts[sig],
		ConsecutiveSameToolFailures: t.failByTool[u.Name],
		ConsecutiveSameSigFailures:  t.failBySig,
		IsTwoStrike:                 twoStrike,
	}
}

// signatureFor builds a stable Sig. ArgsHash = sha256 of key-sorted JSON of
// Input. nil/empty Input → "empty" sentinel so signatures stay comparable.
func signatureFor(u types.ToolUse) ToolCallSignature {
	if len(u.Input) == 0 {
		return ToolCallSignature{Name: u.Name, ArgsHash: "empty"}
	}
	keys := make([]string, 0, len(u.Input))
	for k := range u.Input {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		// json-encode each value so nested maps/slices canonicalize the same way.
		v, err := json.Marshal(u.Input[k])
		if err != nil {
			b.WriteString("<unmarshalable>")
		} else {
			b.Write(v)
		}
		b.WriteByte(';')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return ToolCallSignature{Name: u.Name, ArgsHash: hex.EncodeToString(sum[:8])}
}
