// Package state_machine defines the allowed token lifecycle transition topology.
// AllowedTransitions is the single source of truth for valid state changes.
// The postgres adapter also enforces this at the database level via CAS guards.
package state_machine

// AllowedTransitions maps each state to its valid successor states.
// This is a forward-only DAG — no cycles, no backward moves.
var AllowedTransitions = map[string][]string{
	"DETECTED":        {"DQ_PASSED", "REJECTED"},
	"REJECTED":        {"DQ_PASSED"}, // rescan recovery: re-evaluated token now passes DQ
	"DQ_PASSED":       {"FEATURE_READY", "REJECTED"},
	"FEATURE_READY":   {"EDGE_DETECTED", "REJECTED"},
	"EDGE_DETECTED":   {"VALIDATED", "REJECTED"},
	"VALIDATED":       {"SELECTED", "REJECTED"},
	"SELECTED":        {"EXECUTED", "FAILED"},
	"EXECUTED":        {"POSITION_OPEN", "FAILED"},
	"POSITION_OPEN":   {"POSITION_CLOSED", "FAILED"},
	"POSITION_CLOSED": {"EVALUATED"},
	"EVALUATED":       {},
	"FAILED":          {},
}

// TerminalStates are states from which no further transition is permitted.
// REJECTED is intentionally excluded: rescan re-evaluation allows REJECTED→DQ_PASSED
// recovery when a token passes DQ criteria on a subsequent scan.
var TerminalStates = map[string]bool{
	"EVALUATED": true,
	"FAILED":    true,
}
