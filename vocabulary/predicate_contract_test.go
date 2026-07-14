package vocabulary

import (
	"errors"
	"sort"
	"strings"
	"testing"
)

func TestParsePredicate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		predicate string
		want      PredicateParts
		wantErr   PredicateValidationReason
	}{
		{
			name:      "canonical",
			predicate: "agent.run.entity-id",
			want: PredicateParts{
				Domain:   "agent",
				Category: "run",
				Property: "entity-id",
			},
		},
		{name: "empty", predicate: "", wantErr: PredicateReasonEmpty},
		{name: "one part", predicate: "predicate", wantErr: PredicateReasonArity},
		{name: "two parts", predicate: "agent.run", wantErr: PredicateReasonArity},
		{name: "four parts", predicate: "agent.run.phase.value", wantErr: PredicateReasonArity},
		{name: "empty segment", predicate: "agent..phase", wantErr: PredicateReasonSegmentEmpty},
		{name: "uppercase", predicate: "Agent.run.phase", wantErr: PredicateReasonSegmentStart},
		{name: "underscore", predicate: "agent.run.entity_id", wantErr: PredicateReasonSegmentCharacter},
		{name: "wildcard star", predicate: "agent.run.*", wantErr: PredicateReasonSegmentStart},
		{name: "wildcard greater", predicate: "agent.run.>", wantErr: PredicateReasonSegmentStart},
		{name: "unicode", predicate: "agent.run.snowman-☃", wantErr: PredicateReasonSegmentCharacter},
		{name: "leading digit", predicate: "1agent.run.phase", wantErr: PredicateReasonSegmentStart},
		{name: "leading hyphen", predicate: "agent.run.-phase", wantErr: PredicateReasonSegmentStart},
		{name: "trailing hyphen", predicate: "agent.run.phase-", wantErr: PredicateReasonSegmentHyphen},
		{name: "double hyphen", predicate: "agent.run.phase--name", wantErr: PredicateReasonSegmentHyphen},
		{
			name:      "segment too long",
			predicate: "agent.run." + strings.Repeat("a", MaxPredicateSegmentBytes+1),
			wantErr:   PredicateReasonSegmentLength,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParsePredicate(tt.predicate)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ParsePredicate(%q) unexpected error: %v", tt.predicate, err)
				}
				if got != tt.want {
					t.Fatalf("ParsePredicate(%q) = %#v, want %#v", tt.predicate, got, tt.want)
				}
				return
			}

			var validationErr *PredicateValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("ParsePredicate(%q) error = %T %v, want *PredicateValidationError", tt.predicate, err, err)
			}
			if validationErr.Reason != tt.wantErr {
				t.Fatalf("ParsePredicate(%q) reason = %q, want %q", tt.predicate, validationErr.Reason, tt.wantErr)
			}
			if IsValidPredicate(tt.predicate) {
				t.Fatalf("IsValidPredicate(%q) = true, want false", tt.predicate)
			}
		})
	}
}

func TestParsePredicateMaximumLength(t *testing.T) {
	t.Parallel()

	segment := "a" + strings.Repeat("b", MaxPredicateSegmentBytes-1)
	predicate := strings.Join([]string{segment, segment, segment}, ".")

	if len(predicate) != MaxPredicateBytes {
		t.Fatalf("fixture length = %d, want %d", len(predicate), MaxPredicateBytes)
	}
	if _, err := ParsePredicate(predicate); err != nil {
		t.Fatalf("ParsePredicate(maximum length) unexpected error: %v", err)
	}
}

func FuzzParsePredicateNeverPanics(f *testing.F) {
	for _, seed := range []string{
		"agent.run.phase",
		"agent.run.entity-id",
		"",
		"agent..phase",
		"agent.run.*",
		"agent.run.snowman-☃",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, predicate string) {
		parts, err := ParsePredicate(predicate)
		if err != nil {
			if IsValidPredicate(predicate) {
				t.Fatalf("IsValidPredicate(%q) = true after ParsePredicate rejected it: %v", predicate, err)
			}
			return
		}
		if !IsValidPredicate(predicate) {
			t.Fatalf("IsValidPredicate(%q) = false after ParsePredicate accepted %#v", predicate, parts)
		}
	})
}

func TestRegisteredPredicatesConform(t *testing.T) {
	t.Parallel()

	var invalid []string
	for _, predicate := range ListRegisteredPredicates() {
		if _, err := ParsePredicate(predicate); err != nil {
			invalid = append(invalid, predicate+": "+err.Error())
		}
	}
	if len(invalid) == 0 {
		return
	}

	sort.Strings(invalid)
	t.Fatalf("registered predicates violate the canonical contract:\n%s", strings.Join(invalid, "\n"))
}
