// Package state_machine enforces the token lifecycle state machine.
// ValidateTransition checks topology only — the CAS database guard is
// enforced by adapter.TransitionState.
package state_machine

import (
	"fmt"
	"sort"
)

// ValidateTransition returns an error if the from→to transition is not on
// the AllowedTransitions graph.
func ValidateTransition(from, to string) error {
	targets, known := AllowedTransitions[from]
	if !known {
		return fmt.Errorf("state_machine: unknown source state %q", from)
	}
	for _, t := range targets {
		if t == to {
			return nil
		}
	}
	allowed := sortedCopy(targets)
	return fmt.Errorf("state_machine: transition %s→%s not allowed; valid targets: %v", from, to, allowed)
}

// IsTerminal returns true if state is a terminal state (no further transitions).
func IsTerminal(state string) bool {
	return TerminalStates[state]
}

// ValidStates returns all known state names sorted deterministically.
func ValidStates() []string {
	states := make([]string, 0, len(AllowedTransitions))
	for s := range AllowedTransitions {
		states = append(states, s)
	}
	sort.Strings(states)
	return states
}

func sortedCopy(ss []string) []string {
	out := make([]string, len(ss))
	copy(out, ss)
	sort.Strings(out)
	return out
}
