package agent

// tool_loop_detect.go — detects when the agent is stuck in a repetitive tool-call loop.
//
// Implements four detection strategies learned from LeVik:
//   1. generic_repeat   — same (tool, args) called 10+ times in the sliding window
//   2. no_progress      — same args AND same result 5+ times consecutively
//   3. circuit_breaker  — any single call hash appears 30 times (total trip-wire)
//   4. ping_pong        — two tools alternating A-B-A-B-… for 8+ calls
//
// All comparisons use a 16-char hex prefix of SHA-256 so large argument blobs
// don't consume memory.

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

const (
	loopHistorySize       = 30
	loopWarnThreshold     = 10
	loopCriticalThreshold = 20
	loopCircuitBreaker    = 30
	loopNoProgressConsec  = 5
	loopPingPongLength    = 8
)

// LoopSeverity indicates how serious the detected loop is.
type LoopSeverity int

const (
	LoopNone     LoopSeverity = 0
	LoopWarning  LoopSeverity = 1 // inject hint into context, do not stop
	LoopCritical LoopSeverity = 2 // stop the loop, return user-facing message
)

// LoopDetection is returned after each tool execution batch.
type LoopDetection struct {
	Severity LoopSeverity
	Kind     string
	Message  string // user-facing or LLM-facing description
}

type callRecord struct {
	toolName string
	argsHash string
}

type outcomeRecord struct {
	argsHash   string
	resultHash string
}

// ToolLoopDetector tracks tool calls and detects repetitive patterns.
// Not goroutine-safe — owned by a single runLLMIteration call.
type ToolLoopDetector struct {
	calls    []callRecord    // sliding window (capped at loopHistorySize)
	outcomes []outcomeRecord // parallel window

	// total counts across the entire session (for circuit breaker)
	totalCounts map[string]int // argsHash → count
}

// NewToolLoopDetector returns a ready-to-use detector.
func NewToolLoopDetector() *ToolLoopDetector {
	return &ToolLoopDetector{
		totalCounts: make(map[string]int),
	}
}

// Record adds one tool call (BEFORE execution) to the history.
func (d *ToolLoopDetector) Record(toolName string, args map[string]interface{}) string {
	h := hashToolArgs(toolName, args)
	d.calls = append(d.calls, callRecord{toolName: toolName, argsHash: h})
	if len(d.calls) > loopHistorySize {
		d.calls = d.calls[len(d.calls)-loopHistorySize:]
	}
	d.totalCounts[h]++
	return h
}

// RecordOutcome adds the result hash (AFTER execution) — used for no-progress detection.
func (d *ToolLoopDetector) RecordOutcome(argsHash, result string) {
	rh := hashResult(result)
	d.outcomes = append(d.outcomes, outcomeRecord{argsHash: argsHash, resultHash: rh})
	if len(d.outcomes) > loopHistorySize {
		d.outcomes = d.outcomes[len(d.outcomes)-loopHistorySize:]
	}
}

// Check evaluates all detectors and returns the most severe finding.
// Call this once after ALL tool calls in an iteration have been recorded.
func (d *ToolLoopDetector) Check() LoopDetection {
	if det := d.checkCircuitBreaker(); det.Severity > LoopNone {
		return det
	}
	if det := d.checkGenericRepeat(); det.Severity > LoopNone {
		return det
	}
	if det := d.checkNoProgress(); det.Severity > LoopNone {
		return det
	}
	if det := d.checkPingPong(); det.Severity > LoopNone {
		return det
	}
	return LoopDetection{}
}

// ─── individual detectors ───────────────────────────────────────────────────

func (d *ToolLoopDetector) checkCircuitBreaker() LoopDetection {
	for h, n := range d.totalCounts {
		if n >= loopCircuitBreaker {
			return LoopDetection{
				Severity: LoopCritical,
				Kind:     "circuit_breaker",
				Message: fmt.Sprintf(
					"I've called the same tool %d times without completing the task. "+
						"I'll stop the current approach and summarize what happened.", n),
			}
		}
		_ = h
	}
	return LoopDetection{}
}

func (d *ToolLoopDetector) checkGenericRepeat() LoopDetection {
	if len(d.calls) == 0 {
		return LoopDetection{}
	}
	// Count occurrences of each (name, argsHash) in the window.
	counts := make(map[string]int)
	for _, c := range d.calls {
		key := c.toolName + ":" + c.argsHash
		counts[key]++
	}
	maxCount := 0
	maxName := ""
	for key, n := range counts {
		if n > maxCount {
			maxCount = n
			maxName = key
		}
	}
	_ = maxName
	if maxCount >= loopCriticalThreshold {
		return LoopDetection{
			Severity: LoopCritical,
			Kind:     "generic_repeat",
			Message: fmt.Sprintf(
				"I've made the same tool call %d times in a row. "+
					"I must try a different approach or acknowledge I cannot complete this step.", maxCount),
		}
	}
	if maxCount >= loopWarnThreshold {
		return LoopDetection{
			Severity: LoopWarning,
			Kind:     "generic_repeat",
			Message: fmt.Sprintf(
				"I've called the same tool %d times with identical arguments. "+
					"If the result hasn't changed, I should try a different strategy.", maxCount),
		}
	}
	return LoopDetection{}
}

func (d *ToolLoopDetector) checkNoProgress() LoopDetection {
	if len(d.outcomes) < loopNoProgressConsec {
		return LoopDetection{}
	}
	// Check last N outcomes for same argsHash+resultHash.
	tail := d.outcomes[len(d.outcomes)-loopNoProgressConsec:]
	first := tail[0]
	same := true
	for _, o := range tail[1:] {
		if o.argsHash != first.argsHash || o.resultHash != first.resultHash {
			same = false
			break
		}
	}
	if same {
		return LoopDetection{
			Severity: LoopWarning,
			Kind:     "no_progress",
			Message: fmt.Sprintf(
				"The last %d tool calls produced identical results. "+
					"The environment state isn't changing — I should reassess my approach.",
				loopNoProgressConsec),
		}
	}
	return LoopDetection{}
}

func (d *ToolLoopDetector) checkPingPong() LoopDetection {
	if len(d.calls) < loopPingPongLength {
		return LoopDetection{}
	}
	tail := d.calls[len(d.calls)-loopPingPongLength:]
	// Pattern: A B A B A B ... — tools must alternate
	a, b := tail[0].argsHash, tail[1].argsHash
	if a == b {
		return LoopDetection{}
	}
	pingPong := true
	for i, c := range tail {
		expected := a
		if i%2 == 1 {
			expected = b
		}
		if c.argsHash != expected {
			pingPong = false
			break
		}
	}
	if pingPong {
		return LoopDetection{
			Severity: LoopWarning,
			Kind:     "ping_pong",
			Message: fmt.Sprintf(
				"I've been alternating between two tools %d times with no progress. "+
					"This approach isn't working — I need a fundamentally different strategy.",
				loopPingPongLength),
		}
	}
	return LoopDetection{}
}

// ─── hash helpers ────────────────────────────────────────────────────────────

func hashToolArgs(toolName string, args map[string]interface{}) string {
	payload, _ := json.Marshal(map[string]interface{}{
		"name": toolName,
		"args": args,
	})
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("%x", sum[:8]) // 16 hex chars, enough to distinguish calls
}

func hashResult(result string) string {
	sum := sha256.Sum256([]byte(result))
	return fmt.Sprintf("%x", sum[:8])
}
