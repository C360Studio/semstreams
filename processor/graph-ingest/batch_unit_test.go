package graphingest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
)

// TestAddTriples_ValidationRejectsBeforeCAS pins the validation phase
// in isolation. The CAS path needs a real KV bucket (covered by the
// integration tests in batch_integration_test.go); the validation
// path is pure CPU and runs here in the fast unit-test bucket.
//
// Future ADR-036 stages may layer additional validation (predicate
// allowlist, entity-ID-shape) on top of the empty-Subject /
// empty-Predicate checks. Having a unit harness here means
// regressions surface in milliseconds instead of via integration runs.
func TestAddTriples_ValidationRejectsBeforeCAS(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		name          string
		triples       []message.Triple
		wantInvalid   bool
		wantSubstring string
	}{
		{
			name:    "empty slice — noop, not an error",
			triples: nil,
		},
		{
			name: "empty Subject on first triple",
			triples: []message.Triple{
				{Subject: "", Predicate: "p.q.r", Object: "x", Timestamp: now, Confidence: 1.0},
			},
			wantInvalid:   true,
			wantSubstring: "subject cannot be empty",
		},
		{
			name: "empty Predicate on first triple",
			triples: []message.Triple{
				{Subject: "a.b.c.d.e.001", Predicate: "", Object: "x", Timestamp: now, Confidence: 1.0}, // predicate-audit:invalid {"kind":"stored-predicate","value":"","reason":"empty"}
			},
			wantInvalid:   true,
			wantSubstring: `"" (empty)`,
		},
		{
			name: "valid first, malformed second — whole batch rejected",
			triples: []message.Triple{
				{Subject: "a.b.c.d.e.001", Predicate: "p.q.r", Object: "x", Timestamp: now, Confidence: 1.0},
				{Subject: "", Predicate: "p.q.r", Object: "y", Timestamp: now, Confidence: 1.0},
			},
			wantInvalid:   true,
			wantSubstring: "subject cannot be empty",
		},
		{
			name: "valid first, empty-predicate second — whole batch rejected",
			triples: []message.Triple{
				{Subject: "a.b.c.d.e.001", Predicate: "p.q.r", Object: "x", Timestamp: now, Confidence: 1.0},
				{Subject: "a.b.c.d.e.001", Predicate: "", Object: "y", Timestamp: now, Confidence: 1.0}, // predicate-audit:invalid {"kind":"stored-predicate","value":"","reason":"empty"}
			},
			wantInvalid:   true,
			wantSubstring: `"" (empty)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// AddTriples touches c.entityBucket only AFTER validation
			// passes. Cases that should reject pre-CAS never reach the
			// nil bucket. The two cases that pass validation (empty
			// slice; single valid triple) either short-circuit
			// (nil-slice noop) or fail at CAS — we only assert the
			// validation phase here.
			c := &Component{}
			written, failed, err := c.AddTriples(context.Background(), tt.triples)

			if tt.wantInvalid {
				if err == nil {
					t.Fatalf("expected validation error, got nil (written=%d failed=%v)", written, failed)
				}
				if !errs.IsInvalid(err) {
					t.Errorf("expected ErrorInvalid class, got %T: %v", err, err)
				}
				if tt.wantSubstring != "" && !strings.Contains(err.Error(), tt.wantSubstring) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantSubstring)
				}
				if written != 0 {
					t.Errorf("validation rejection must not commit any triples (written=%d)", written)
				}
				if len(failed) != 0 {
					t.Errorf("validation rejection has no FailedSubjects (got %v)", failed)
				}
				return
			}

			// Empty-slice case: nil-slice noop, no error, no work.
			if len(tt.triples) == 0 {
				if err != nil {
					t.Errorf("empty batch should be a noop, got err=%v", err)
				}
				if written != 0 {
					t.Errorf("empty batch must not commit anything (written=%d)", written)
				}
			}
		})
	}
}
