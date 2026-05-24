package lifecycle

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// droneTransitions is the worked-example table from ADR-047 / doc.go.
// Reused across tests so a single canonical fixture validates the
// surface; if the example diverges from what code accepts the test
// suite catches it.
var droneTransitions = Transitions{
	"planning":  {"flying", "aborted"},
	"flying":    {"capturing", "landing", "aborted"},
	"capturing": {"flying"},
	"landing":   {"completed", "failed"},
	"completed": {},
	"failed":    {},
	"aborted":   {},
}

func TestTransitions_Validate_AcceptsCanonicalExample(t *testing.T) {
	if err := droneTransitions.Validate(); err != nil {
		t.Fatalf("ADR-047 worked example must validate, got %v", err)
	}
}

func TestTransitions_Validate_RejectsEmpty(t *testing.T) {
	if err := (Transitions{}).Validate(); !errors.Is(err, ErrInvalidTransitionsTable) {
		t.Fatalf("empty table should error with ErrInvalidTransitionsTable, got %v", err)
	}
}

func TestTransitions_Validate_RejectsUndeclaredTarget(t *testing.T) {
	// Out-edge to "exploded" but "exploded" never declared as a key.
	// Most common typo class — catches it at Register time, not at
	// the first transition attempt in production.
	bad := Transitions{
		"flying":  {"landing", "exploded"},
		"landing": {},
	}
	err := bad.Validate()
	if !errors.Is(err, ErrInvalidTransitionsTable) {
		t.Fatalf("undeclared target should error, got %v", err)
	}
	if err.Error() == "" || !strings.Contains(err.Error(), "exploded") {
		t.Errorf("error should mention the undeclared phase %q for operator debugging, got %q",
			"exploded", err)
	}
}

func TestTransitions_Validate_RejectsDuplicateOutEdge(t *testing.T) {
	bad := Transitions{
		"a": {"b", "b"},
		"b": {},
	}
	if err := bad.Validate(); !errors.Is(err, ErrInvalidTransitionsTable) {
		t.Fatalf("duplicate out-edges should error, got %v", err)
	}
}

func TestTransitions_Validate_RejectsEmptyPhaseName(t *testing.T) {
	bad := Transitions{
		"":  {"a"},
		"a": {},
	}
	if err := bad.Validate(); !errors.Is(err, ErrInvalidTransitionsTable) {
		t.Fatalf("empty-string phase key should error, got %v", err)
	}
}

func TestTransitions_Validate_RejectsEmptyTargetName(t *testing.T) {
	bad := Transitions{
		"a": {""},
		"":  {},
	}
	if err := bad.Validate(); !errors.Is(err, ErrInvalidTransitionsTable) {
		t.Fatalf("empty-string out-edge should error, got %v", err)
	}
}

func TestTransitions_Validate_AcceptsCycles(t *testing.T) {
	// capturing → flying → capturing is intentional in the drone
	// example. Validate must not flag legitimate cycles.
	withCycle := Transitions{
		"flying":    {"capturing"},
		"capturing": {"flying"},
	}
	if err := withCycle.Validate(); err != nil {
		t.Fatalf("cycles must validate (drone capturing↔flying example), got %v", err)
	}
}

func TestTransitions_IsTerminal(t *testing.T) {
	tests := []struct {
		phase string
		want  bool
	}{
		{"completed", true},
		{"failed", true},
		{"aborted", true},
		{"flying", false},
		{"planning", false},
		// Unknown phases default to terminal — defensive fallback so
		// a misconfigured entity can't transition unexpectedly.
		{"never-declared", true},
	}
	for _, tt := range tests {
		t.Run(tt.phase, func(t *testing.T) {
			if got := droneTransitions.IsTerminal(tt.phase); got != tt.want {
				t.Errorf("IsTerminal(%q) = %v, want %v", tt.phase, got, tt.want)
			}
		})
	}
}

func TestTransitions_IsValidTransition(t *testing.T) {
	tests := []struct {
		from, to string
		want     bool
	}{
		{"planning", "flying", true},
		{"flying", "capturing", true},
		{"capturing", "flying", true}, // cycle is legitimate
		{"landing", "completed", true},
		{"planning", "completed", false},    // not a declared edge
		{"completed", "flying", false},      // terminal phase, no out-edges
		{"never-declared", "flying", false}, // unknown source phase
		{"flying", "never-declared", false}, // unknown target phase (not in flying's edges)
	}
	for _, tt := range tests {
		t.Run(tt.from+"->"+tt.to, func(t *testing.T) {
			if got := droneTransitions.IsValidTransition(tt.from, tt.to); got != tt.want {
				t.Errorf("IsValidTransition(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestTransitions_Phases_StableSorted(t *testing.T) {
	got := droneTransitions.Phases()
	want := []string{"aborted", "capturing", "completed", "failed", "flying", "landing", "planning"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Phases() = %v, want %v (sorted)", got, want)
	}
}

func TestTransitions_TerminalPhases_StableSorted(t *testing.T) {
	got := droneTransitions.TerminalPhases()
	want := []string{"aborted", "completed", "failed"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TerminalPhases() = %v, want %v (sorted)", got, want)
	}
}
