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
	Sig                           ToolCallSignature
	RepeatCount                   int  // how many times this exact Sig has fired this Run
	ConsecutiveSameToolFailures   int  // streak of failures on the same tool name (any args)
	ConsecutiveSameSigFailures    int  // streak of failures on the same (tool,args) sig
	SameResultCount               int  // successful calls of this Sig returning byte-identical output
	ConsecutiveSameFamilyFailures int  // streak of failures sharing one (tool, error-family) key
	IsTwoStrike                   bool // same Sig failed twice in a row → nudge (no longer bails)
}

// repeatTracker is a per-Run signal collector. cheap to allocate.
type repeatTracker struct {
	// counts every signature seen this Run (success or fail).
	sigCounts map[ToolCallSignature]int
	// fail-streak by tool name; resets when that tool succeeds.
	failByTool map[string]int
	// fail-streak by (tool,args) sig; resets on success or different sig.
	failBySig int
	// per-sig count of successful calls returning byte-identical output, and
	// the last output hash seen. fuels the identical-read-loop guard: a read
	// repeated with the same bytes back teaches the model nothing.
	sameResult map[ToolCallSignature]int
	lastResult map[ToolCallSignature]string
	// last signature seen and whether it failed. used for two-strike detect.
	lastSig    ToolCallSignature
	lastFailed bool
	hasLast    bool
	// error-family streak: the last failing family key and the run of failures
	// landing on it. reset by any success. see error_family.go.
	familyKey    string
	familyStreak int
	familyActive bool
}

func newRepeatTracker() *repeatTracker {
	return &repeatTracker{
		sigCounts:  map[ToolCallSignature]int{},
		failByTool: map[string]int{},
		sameResult: map[ToolCallSignature]int{},
		lastResult: map[ToolCallSignature]string{},
	}
}

// Observe records one tool call and returns aggregates. Safe to call with
// any ToolUse; never errors. Result-blind shorthand for ObserveWithResult.
func (t *repeatTracker) Observe(u types.ToolUse, isErr bool) Observation {
	return t.ObserveWithResult(u, isErr, "")
}

// ObserveWithResult is Observe with the raw tool-result content. The content
// fuels the identical-output streak (hashed internally) and the error-family
// streak. Empty content skips output tracking but still permits family
// clustering of empty-body errors.
func (t *repeatTracker) ObserveWithResult(u types.ToolUse, isErr bool, resultContent string) Observation {
	sig := signatureFor(u)
	resultHash := hashResult(resultContent)
	t.sigCounts[sig]++
	if isErr {
		t.failByTool[u.Name]++
		t.sameResult[sig] = 0
	} else {
		t.failByTool[u.Name] = 0
		if resultHash != "" && t.lastResult[sig] == resultHash {
			t.sameResult[sig]++
		} else if resultHash != "" {
			t.sameResult[sig] = 1
			t.lastResult[sig] = resultHash
		}
	}
	familyStreak := t.observeFamily(u.Name, isErr, resultContent)
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
		Sig:                           sig,
		RepeatCount:                   t.sigCounts[sig],
		ConsecutiveSameToolFailures:   t.failByTool[u.Name],
		ConsecutiveSameSigFailures:    t.failBySig,
		SameResultCount:               t.sameResult[sig],
		ConsecutiveSameFamilyFailures: familyStreak,
		IsTwoStrike:                   twoStrike,
	}
}

// observeFamily folds one result into the error-family streak and returns the
// current run length. A success clears the streak (the tool made progress); a
// failure extends it only when its (tool, normalized-shape) key matches the
// previous failing family, otherwise the streak restarts at 1.
func (t *repeatTracker) observeFamily(toolName string, isErr bool, content string) int {
	if !isErr {
		t.familyStreak = 0
		t.familyKey = ""
		t.familyActive = false
		return 0
	}
	key := errorFamilyKey(toolName, content)
	if t.familyActive && t.familyKey == key {
		t.familyStreak++
	} else {
		t.familyStreak = 1
	}
	t.familyKey = key
	t.familyActive = true
	return t.familyStreak
}

// batchKey returns a stable hash for one step's set of tool calls: each call
// reduced to its (name, canonical-args) signature, sorted, then hashed. The
// result is order-independent, so a step issuing the same calls in any order
// keys identically — a single-call step and a multi-call step are each one
// deterministic key. An empty step gets the "empty" sentinel. Feeds the
// ABAB-cycle tracker in cycle_detect.go.
func batchKey(uses []types.ToolUse) string {
	if len(uses) == 0 {
		return "empty"
	}
	parts := make([]string, 0, len(uses))
	for _, u := range uses {
		sig := signatureFor(u)
		parts = append(parts, sig.Name+":"+sig.ArgsHash)
	}
	sort.Strings(parts)
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// hashResult fingerprints a tool result body for the identical-output streak.
func hashResult(content string) string {
	if content == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:8])
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
