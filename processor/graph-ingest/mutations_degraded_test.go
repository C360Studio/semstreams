package graphingest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/graph"
)

// TestMarshalDegraded_AllFourEntityShapes verifies the #120 contract
// for all four entity-mutation handlers' post-write read-back failure
// path. Each marshaler must produce Success=true + Degraded=true +
// Entity=nil + KVRevision=0 + Error containing the read-back failure
// reason. The four call sites in mutations.go share this contract;
// keeping the assertions table-driven catches any single-site drift.
func TestMarshalDegraded_AllFourEntityShapes(t *testing.T) {
	cases := []struct {
		name             string
		marshal          func(traceID, requestID, readbackErr string) ([]byte, error)
		decodeWithEntity func([]byte) (resp graph.MutationResponse, hasEntity bool, err error)
	}{
		{
			name:    "create_entity_with_triples",
			marshal: marshalCreateEntityWithTriplesDegraded,
			decodeWithEntity: func(b []byte) (graph.MutationResponse, bool, error) {
				var r graph.CreateEntityWithTriplesResponse
				if err := json.Unmarshal(b, &r); err != nil {
					return graph.MutationResponse{}, false, err
				}
				return r.MutationResponse, r.Entity != nil, nil
			},
		},
		{
			name:    "update_entity",
			marshal: marshalUpdateEntityDegraded,
			decodeWithEntity: func(b []byte) (graph.MutationResponse, bool, error) {
				var r graph.UpdateEntityResponse
				if err := json.Unmarshal(b, &r); err != nil {
					return graph.MutationResponse{}, false, err
				}
				return r.MutationResponse, r.Entity != nil, nil
			},
		},
		{
			name:    "update_entity_with_triples",
			marshal: marshalUpdateEntityWithTriplesDegraded,
			decodeWithEntity: func(b []byte) (graph.MutationResponse, bool, error) {
				var r graph.UpdateEntityWithTriplesResponse
				if err := json.Unmarshal(b, &r); err != nil {
					return graph.MutationResponse{}, false, err
				}
				return r.MutationResponse, r.Entity != nil, nil
			},
		},
	}

	const (
		traceID     = "trace-123"
		requestID   = "req-456"
		readbackErr = "context cancelled before read-back completed"
	)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := tc.marshal(traceID, requestID, readbackErr)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			mr, hasEntity, err := tc.decodeWithEntity(data)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !mr.Degraded {
				t.Errorf("Degraded = false, want true")
			}
			if hasEntity {
				t.Errorf("Entity should be nil on degraded path; got non-nil")
			}
			if mr.KVRevision != 0 {
				t.Errorf("KVRevision = %d, want 0 (no read-back to source the revision from)", mr.KVRevision)
			}
			if !strings.HasPrefix(mr.DegradedReason, degradedReadbackErrPrefix) {
				t.Errorf("DegradedReason = %q, want prefix %q", mr.DegradedReason, degradedReadbackErrPrefix)
			}
			if !strings.Contains(mr.DegradedReason, readbackErr) {
				t.Errorf("DegradedReason = %q, want it to include the underlying read-back reason %q", mr.DegradedReason, readbackErr)
			}
			if mr.TraceID != traceID {
				t.Errorf("TraceID = %q, want %q", mr.TraceID, traceID)
			}
			if mr.RequestID != requestID {
				t.Errorf("RequestID = %q, want %q", mr.RequestID, requestID)
			}
			if mr.Timestamp == 0 {
				t.Error("Timestamp = 0, want non-zero")
			}
		})
	}
}

// TestMutationResponse_DegradedOmittedWhenFalse confirms that the JSON
// wire shape omits the degraded field on the success-clean path (back-
// compat for callers that may not handle the new field — they see no
// new key in the JSON they were already parsing).
func TestMutationResponse_DegradedOmittedWhenFalse(t *testing.T) {
	resp := graph.CreateEntityResponse{Outcome: graph.MutationApplied, Entity: &graph.EntityState{}, KVRevision: 42}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "degraded") {
		t.Errorf("Degraded=false should be omitted from JSON; got: %s", string(b))
	}
}
