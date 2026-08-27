package graphingest

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
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
		{name: "empty slice is invalid", triples: nil, wantInvalid: true, wantSubstring: "triples cannot be empty"},
		{
			name: "empty Subject on first triple",
			triples: []message.Triple{
				{Subject: "", Predicate: "p.q.r", Object: "x", Timestamp: now, Confidence: 1.0},
			},
			wantInvalid:   true,
			wantSubstring: "entity state contract violation: id",
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
			wantSubstring: "entity state contract violation: id",
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

			// Canonical append touches c.entityBucket only after validating the
			// complete request, so these cases prove rejection is pre-I/O.
			c := withTestRegistry(t, &Component{})
			data, marshalErr := json.Marshal(graph.AppendTriplesRequest{Triples: tt.triples})
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			_, err := c.handleCanonicalAppend(context.Background(), data)

			if tt.wantInvalid {
				if err == nil {
					t.Fatal("expected validation error, got nil")
				}
				if !errs.IsInvalid(err) {
					t.Errorf("expected ErrorInvalid class, got %T: %v", err, err)
				}
				if tt.wantSubstring != "" && !strings.Contains(err.Error(), tt.wantSubstring) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantSubstring)
				}
				return
			}
		})
	}
}
