package loop

import (
	"encoding/json"
	"maps"
	"slices"
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
	RepeatCount                 int  // how many times this exact Sig has fired this Run
	ConsecutiveSameToolFailures int  // streak of failures on the same tool name (any args)
	ConsecutiveSameSigFailures  int  // streak of failures on the same (tool,args) sig
	SameResultCount             int  // successful calls of this Sig returning byte-identical output
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
	// per-sig count of successful calls returning byte-identical output, and
	// the last output hash seen. fuels the identical-read-loop guard: a read
	// repeated with the same bytes back teaches the model nothing.
	sameResult map[ToolCallSignature]int
	lastResult map[ToolCallSignature]string
	// last signature seen and whether it failed. used for two-strike detect.
	lastSig    ToolCallSignature
	lastFailed bool
	// failure fingerprint → times a test run returned it. see test_fail_loop.go.
	testFails map[string]int
	// failure keys already counted in the batch being observed; nil outside a
	// batch. one dispatch batch is one step for the model, so N parallel calls
	// failing the same way count once toward the failure streaks.
	batchFails map[string]bool
}

// beginBatch starts one dispatch batch: each tool name and each call signature
// extends its failure streak at most once until endBatch.
func (t *repeatTracker) beginBatch() { t.batchFails = map[string]bool{} }

func (t *repeatTracker) endBatch() { t.batchFails = nil }

// firstFailIn reports whether key has not yet extended a streak in this batch.
// outside a batch every failure counts.
func (t *repeatTracker) firstFailIn(key string) bool {
	if t.batchFails == nil {
		return true
	}
	if t.batchFails[key] {
		return false
	}
	t.batchFails[key] = true
	return true
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
// fuels the identical-output streak (hashed internally). empty content skips
// output tracking.
func (t *repeatTracker) ObserveWithResult(u types.ToolUse, isErr bool, resultContent string) Observation {
	sig := signatureFor(u)
	resultHash := ""
	if resultContent != "" {
		resultHash = hash8(resultContent)
	}
	t.sigCounts[sig]++
	if isErr {
		if t.firstFailIn("tool:" + u.Name) {
			t.failByTool[u.Name]++
		}
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
	twoStrike := isErr && t.lastFailed && t.lastSig == sig
	// failBySig: extend streak only when current is err AND sig matches last err sig.
	sigKey := "sig:" + sig.Name + ":" + sig.ArgsHash
	switch {
	case twoStrike:
		if t.firstFailIn(sigKey) {
			t.failBySig++
		}
	case isErr:
		t.failBySig = 1
		t.firstFailIn(sigKey)
	default:
		t.failBySig = 0
	}
	t.lastSig, t.lastFailed = sig, isErr
	return Observation{
		RepeatCount:                 t.sigCounts[sig],
		ConsecutiveSameToolFailures: t.failByTool[u.Name],
		ConsecutiveSameSigFailures:  t.failBySig,
		SameResultCount:             t.sameResult[sig],
		IsTwoStrike:                 twoStrike,
	}
}

// signatureFor builds a stable Sig. ArgsHash = sha256 of key-sorted JSON of
// Input. nil/empty Input → "empty" sentinel so signatures stay comparable.
func signatureFor(u types.ToolUse) ToolCallSignature {
	// the label is worded fresh each time; the same call must still repeat
	in := u.Args()
	if len(in) == 0 {
		return ToolCallSignature{Name: u.Name, ArgsHash: "empty"}
	}
	var b strings.Builder
	for _, k := range slices.Sorted(maps.Keys(in)) {
		b.WriteString(k)
		b.WriteByte('=')
		// json-encode each value so nested maps/slices canonicalize the same way.
		v, err := json.Marshal(in[k])
		if err != nil {
			b.WriteString("<unmarshalable>")
		} else {
			b.Write(v)
		}
		b.WriteByte(';')
	}
	return ToolCallSignature{Name: u.Name, ArgsHash: hash8(b.String())}
}
