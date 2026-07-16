package semantictest

import (
	"errors"
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
)

func TestEntityIDPreservesExplicitPositions(t *testing.T) {
	t.Parallel()

	const want = "Acme.ops_platform.robotics.gcs-1.drone.unit_7"
	if got := EntityID(t, "Acme", "ops_platform", "robotics", "gcs-1", "drone", "unit_7"); got != want {
		t.Fatalf("EntityID() = %q, want exact %q", got, want)
	}
}

func TestValidateEntityIDFixtureRejectsExactInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		parts      [6]string
		want       string
		wantReason string
	}{
		{
			name:       "leading forbidden byte",
			parts:      [6]string{"_acme", "ops", "robotics", "gcs", "drone", "1"},
			want:       "_acme.ops.robotics.gcs.drone.1",
			wantReason: semtypes.EntityIDReasonFirstByte,
		},
		{
			name:       "empty explicit position",
			parts:      [6]string{"acme", "ops", "", "gcs", "drone", "1"},
			want:       "acme.ops..gcs.drone.1",
			wantReason: semtypes.EntityIDReasonEmptySegment,
		},
		{
			name:       "forbidden byte is not normalized",
			parts:      [6]string{"acme", "ops", "robot ics", "gcs", "drone", "1"},
			want:       "acme.ops.robot ics.gcs.drone.1",
			wantReason: semtypes.EntityIDReasonAlphabet,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := validateEntityIDFixture(
				test.parts[0], test.parts[1], test.parts[2],
				test.parts[3], test.parts[4], test.parts[5],
			)
			if got != test.want {
				t.Fatalf("joined fixture = %q, want exact %q", got, test.want)
			}
			if err == nil {
				t.Fatal("validateEntityIDFixture() error = nil, want authoritative rejection")
			}
			var classified *errs.ClassifiedError
			if !errors.As(err, &classified) {
				t.Fatalf("error type = %T, want *errs.ClassifiedError", err)
			}
			if classified.Code != semtypes.ErrorCodeEntityIDInvalid {
				t.Fatalf("error code = %q, want %q", classified.Code, semtypes.ErrorCodeEntityIDInvalid)
			}
			if reason := classified.Detail[semtypes.EntityIDDetailReason]; reason != test.wantReason {
				t.Fatalf("error reason = %v, want %q", reason, test.wantReason)
			}
		})
	}
}

func TestPredicatePreservesExplicitPositions(t *testing.T) {
	t.Parallel()

	const want = "agentic.loop.run-state"
	if got := Predicate(t, "agentic", "loop", "run-state"); got != want {
		t.Fatalf("Predicate() = %q, want exact %q", got, want)
	}
}

func TestValidatePredicateFixtureRejectsExactInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		parts      [3]string
		want       string
		wantReason vocabulary.PredicateValidationReason
	}{
		{
			name:       "uppercase is not lowered",
			parts:      [3]string{"Agentic", "loop", "state"},
			want:       "Agentic.loop.state",
			wantReason: vocabulary.PredicateReasonSegmentStart,
		},
		{
			name:       "underscore is not replaced",
			parts:      [3]string{"agentic", "loop_state", "current"},
			want:       "agentic.loop_state.current",
			wantReason: vocabulary.PredicateReasonSegmentCharacter,
		},
		{
			name:       "trailing hyphen is not trimmed",
			parts:      [3]string{"agentic", "loop", "state-"},
			want:       "agentic.loop.state-",
			wantReason: vocabulary.PredicateReasonSegmentHyphen,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := validatePredicateFixture(test.parts[0], test.parts[1], test.parts[2])
			if got != test.want {
				t.Fatalf("joined fixture = %q, want exact %q", got, test.want)
			}
			if err == nil {
				t.Fatal("validatePredicateFixture() error = nil, want authoritative rejection")
			}
			var validationError *vocabulary.PredicateValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("error type = %T, want *vocabulary.PredicateValidationError", err)
			}
			if validationError.Predicate != test.want {
				t.Fatalf("rejected predicate = %q, want exact %q", validationError.Predicate, test.want)
			}
			if validationError.Reason != test.wantReason {
				t.Fatalf("error reason = %q, want %q", validationError.Reason, test.wantReason)
			}
		})
	}
}
