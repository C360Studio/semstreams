package lifecycle

import (
	"fmt"
	"slices"
	"sort"
)

// Transitions declares the valid phase transitions for a workflow
// type. Keys are source phases; values are slices of valid destination
// phases. Terminal phases have empty out-edges.
//
// Example (drone-survey mission):
//
//	transitions := lifecycle.Transitions{
//	    "planning":   {"flying", "aborted"},
//	    "flying":     {"capturing", "landing", "aborted"},
//	    "capturing":  {"flying"},
//	    "landing":    {"completed", "failed"},
//	    "completed":  {},  // terminal
//	    "failed":     {},  // terminal
//	    "aborted":    {},  // terminal
//	}
//
// The table is checked structurally by Validate at Register time and
// then consulted by Manager.Transition on every transition request.
// Edges not declared in the table are rejected with ErrInvalidTransition.
type Transitions map[string][]string

// Validate checks structural consistency of the transitions table.
// Returns ErrInvalidTransitionsTable wrapped with a descriptive message
// when:
//   - The table is empty (no phases declared)
//   - An out-edge references a phase that isn't a key in the table
//     (typo catcher — every reachable phase must be declared, even
//     terminal ones with empty out-edges)
//   - A phase has duplicate out-edges (e.g. {"flying": ["landing",
//     "landing"]}) — defends against copy-paste bugs in rule packs
//
// Returns nil when the table is internally consistent.
//
// Note: Validate does NOT check reachability from an initial phase
// (apps may have multiple entry points) or absence of cycles (cycles
// are legitimate — e.g. capturing → flying → capturing in the drone
// example).
func (t Transitions) Validate() error {
	if len(t) == 0 {
		return fmt.Errorf("%w: transitions table is empty", ErrInvalidTransitionsTable)
	}

	for phase, outEdges := range t {
		if phase == "" {
			return fmt.Errorf("%w: empty-string phase key not allowed", ErrInvalidTransitionsTable)
		}

		seen := make(map[string]struct{}, len(outEdges))
		for _, target := range outEdges {
			if target == "" {
				return fmt.Errorf("%w: phase %q has empty-string out-edge", ErrInvalidTransitionsTable, phase)
			}
			if _, dup := seen[target]; dup {
				return fmt.Errorf("%w: phase %q has duplicate out-edge to %q",
					ErrInvalidTransitionsTable, phase, target)
			}
			seen[target] = struct{}{}

			if _, declared := t[target]; !declared {
				return fmt.Errorf("%w: phase %q has out-edge to undeclared phase %q (declare it as a key with empty out-edges if terminal)",
					ErrInvalidTransitionsTable, phase, target)
			}
		}
	}
	return nil
}

// IsTerminal returns true when the given phase has no declared
// out-edges in the table (or isn't declared at all — defensive
// fallback to treat unknown phases as terminal so a misconfigured
// instance can't transition unexpectedly).
//
// Apps that derive Participant.IsTerminal from the Transitions table
// can use this helper directly:
//
//	func (m *MissionState) IsTerminal() bool {
//	    return missionTransitions.IsTerminal(m.Phase())
//	}
func (t Transitions) IsTerminal(phase string) bool {
	outEdges, declared := t[phase]
	if !declared {
		return true
	}
	return len(outEdges) == 0
}

// IsValidTransition returns true when (from → to) is a declared edge
// in the table. Used by Manager.Transition to gate transitions, but
// safe for app-side use too (e.g. to filter operator-facing
// "available next phases" UI).
func (t Transitions) IsValidTransition(from, to string) bool {
	outEdges, declared := t[from]
	if !declared {
		return false
	}
	return slices.Contains(outEdges, to)
}

// Phases returns the set of declared phases in sorted order. Useful
// for operator dashboards rendering state-machine diagrams or for
// validation tooling enumerating the workflow's surface.
func (t Transitions) Phases() []string {
	phases := make([]string, 0, len(t))
	for phase := range t {
		phases = append(phases, phase)
	}
	sort.Strings(phases)
	return phases
}

// TerminalPhases returns the subset of declared phases that are
// terminal (no out-edges), sorted. Operator dashboards use this to
// group instances by terminal-vs-active when ListOptions.Active is
// not set.
func (t Transitions) TerminalPhases() []string {
	terminal := make([]string, 0)
	for phase, outEdges := range t {
		if len(outEdges) == 0 {
			terminal = append(terminal, phase)
		}
	}
	sort.Strings(terminal)
	return terminal
}
