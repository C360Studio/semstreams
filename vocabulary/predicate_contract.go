package vocabulary

import (
	"fmt"
	"strings"
)

const (
	// MaxPredicateSegmentBytes bounds each domain/category/property token.
	MaxPredicateSegmentBytes = 64
	// MaxPredicateBytes is three maximum-length segments plus two separators.
	MaxPredicateBytes = 3*MaxPredicateSegmentBytes + 2
)

// PredicateValidationReason is a stable machine-readable predicate rejection reason.
type PredicateValidationReason string

const (
	// PredicateReasonEmpty means the identity is empty.
	PredicateReasonEmpty PredicateValidationReason = "empty"
	// PredicateReasonLength means the complete identity exceeds its byte bound.
	PredicateReasonLength PredicateValidationReason = "length"
	// PredicateReasonArity means the identity does not have exactly three segments.
	PredicateReasonArity PredicateValidationReason = "arity"
	// PredicateReasonSegmentEmpty means one semantic segment is empty.
	PredicateReasonSegmentEmpty PredicateValidationReason = "segment_empty"
	// PredicateReasonSegmentLength means one segment exceeds its byte bound.
	PredicateReasonSegmentLength PredicateValidationReason = "segment_length"
	// PredicateReasonSegmentStart means a segment does not start with lowercase ASCII.
	PredicateReasonSegmentStart PredicateValidationReason = "segment_start"
	// PredicateReasonSegmentCharacter means a segment contains a forbidden character.
	PredicateReasonSegmentCharacter PredicateValidationReason = "segment_character"
	// PredicateReasonSegmentHyphen means a segment has invalid hyphen placement.
	PredicateReasonSegmentHyphen PredicateValidationReason = "segment_hyphen"
)

// PredicateParts is the canonical semantic decomposition of a predicate.
type PredicateParts struct {
	Domain   string
	Category string
	Property string
}

// String reconstructs the exact canonical predicate identity.
func (p PredicateParts) String() string {
	return p.Domain + "." + p.Category + "." + p.Property
}

// PredicateValidationError reports why a predicate does not satisfy the
// canonical lower-kebab domain.category.property contract.
type PredicateValidationError struct {
	Predicate string
	Reason    PredicateValidationReason
	Segment   int
}

// Error implements error.
func (e *PredicateValidationError) Error() string {
	if e.Segment >= 0 {
		return fmt.Sprintf("predicate %q is invalid: %s at segment %d", e.Predicate, e.Reason, e.Segment+1)
	}
	return fmt.Sprintf("predicate %q is invalid: %s", e.Predicate, e.Reason)
}

// ParsePredicate validates and decomposes a canonical predicate. Each of the
// three segments is lower-kebab ASCII: [a-z][a-z0-9]*(-[a-z0-9]+)*.
func ParsePredicate(predicate string) (PredicateParts, error) {
	if predicate == "" {
		return PredicateParts{}, newPredicateValidationError(predicate, PredicateReasonEmpty, -1)
	}
	if len(predicate) > MaxPredicateBytes {
		return PredicateParts{}, newPredicateValidationError(predicate, PredicateReasonLength, -1)
	}

	segments := strings.Split(predicate, ".")
	if len(segments) != 3 {
		return PredicateParts{}, newPredicateValidationError(predicate, PredicateReasonArity, -1)
	}
	for i, segment := range segments {
		if err := validatePredicateSegment(predicate, segment, i); err != nil {
			return PredicateParts{}, err
		}
	}

	return PredicateParts{
		Domain:   segments[0],
		Category: segments[1],
		Property: segments[2],
	}, nil
}

func validatePredicateSegment(predicate, segment string, index int) error {
	if segment == "" {
		return newPredicateValidationError(predicate, PredicateReasonSegmentEmpty, index)
	}
	if len(segment) > MaxPredicateSegmentBytes {
		return newPredicateValidationError(predicate, PredicateReasonSegmentLength, index)
	}
	if !isLowerASCII(segment[0]) {
		return newPredicateValidationError(predicate, PredicateReasonSegmentStart, index)
	}

	previousHyphen := false
	for i := 1; i < len(segment); i++ {
		char := segment[i]
		switch {
		case isLowerASCII(char), isDigitASCII(char):
			previousHyphen = false
		case char == '-':
			if previousHyphen || i == len(segment)-1 {
				return newPredicateValidationError(predicate, PredicateReasonSegmentHyphen, index)
			}
			previousHyphen = true
		default:
			return newPredicateValidationError(predicate, PredicateReasonSegmentCharacter, index)
		}
	}
	return nil
}

func isLowerASCII(char byte) bool {
	return char >= 'a' && char <= 'z'
}

func isDigitASCII(char byte) bool {
	return char >= '0' && char <= '9'
}

func newPredicateValidationError(
	predicate string,
	reason PredicateValidationReason,
	segment int,
) *PredicateValidationError {
	return &PredicateValidationError{
		Predicate: predicate,
		Reason:    reason,
		Segment:   segment,
	}
}
