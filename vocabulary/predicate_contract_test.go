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
		{name: "empty", predicate: "", wantErr: PredicateReasonEmpty},                                      // predicate-audit:invalid {"kind":"stored-predicate","value":"","reason":"empty"}
		{name: "one part", predicate: "predicate", wantErr: PredicateReasonArity},                          // predicate-audit:invalid {"kind":"stored-predicate","value":"predicate","reason":"arity"}
		{name: "two parts", predicate: "agent.run", wantErr: PredicateReasonArity},                         // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.run","reason":"arity"}
		{name: "four parts", predicate: "agent.run.phase.value", wantErr: PredicateReasonArity},            // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.run.phase.value","reason":"arity"}
		{name: "empty segment", predicate: "agent..phase", wantErr: PredicateReasonSegmentEmpty},           // predicate-audit:invalid {"kind":"stored-predicate","value":"agent..phase","reason":"segment_empty"}
		{name: "uppercase", predicate: "Agent.run.phase", wantErr: PredicateReasonSegmentStart},            // predicate-audit:invalid {"kind":"stored-predicate","value":"Agent.run.phase","reason":"segment_start"}
		{name: "underscore", predicate: "agent.run.entity_id", wantErr: PredicateReasonSegmentCharacter},   // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.run.entity_id","reason":"segment_character"}
		{name: "wildcard star", predicate: "agent.run.*", wantErr: PredicateReasonSegmentStart},            // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.run.*","reason":"segment_start"}
		{name: "wildcard greater", predicate: "agent.run.>", wantErr: PredicateReasonSegmentStart},         // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.run.>","reason":"segment_start"}
		{name: "unicode", predicate: "agent.run.snowman-☃", wantErr: PredicateReasonSegmentCharacter},      // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.run.snowman-☃","reason":"segment_character"}
		{name: "leading digit", predicate: "1agent.run.phase", wantErr: PredicateReasonSegmentStart},       // predicate-audit:invalid {"kind":"stored-predicate","value":"1agent.run.phase","reason":"segment_start"}
		{name: "leading hyphen", predicate: "agent.run.-phase", wantErr: PredicateReasonSegmentStart},      // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.run.-phase","reason":"segment_start"}
		{name: "trailing hyphen", predicate: "agent.run.phase-", wantErr: PredicateReasonSegmentHyphen},    // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.run.phase-","reason":"segment_hyphen"}
		{name: "double hyphen", predicate: "agent.run.phase--name", wantErr: PredicateReasonSegmentHyphen}, // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.run.phase--name","reason":"segment_hyphen"}
		{
			name:      "segment too long",
			predicate: "agent.run." + strings.Repeat("a", MaxPredicateSegmentBytes+1), // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.run.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","reason":"segment_length"}
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

	const segment = "abbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const predicate = segment + "." + segment + "." + segment

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
		"",                    // predicate-audit:invalid {"kind":"stored-predicate","value":"","reason":"empty"}
		"agent..phase",        // predicate-audit:invalid {"kind":"stored-predicate","value":"agent..phase","reason":"segment_empty"}
		"agent.run.*",         // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.run.*","reason":"segment_start"}
		"agent.run.snowman-☃", // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.run.snowman-☃","reason":"segment_character"}
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
