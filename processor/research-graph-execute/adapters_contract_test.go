package researchexecute

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/fusion"
)

type sliceEExecuteRequester struct {
	response []byte
}

func (r *sliceEExecuteRequester) RequestClassified(context.Context, string, []byte, time.Duration) ([]byte, error) {
	return r.response, nil
}

func TestSliceEExecuteBareAndEnvelopedRepliesAgree(t *testing.T) {
	entityID := "acme.ops.robotics.gcs.drone.001"
	neighborID := "acme.ops.robotics.gcs.sensor.002"
	tests := []struct {
		name    string
		subject string
		payload any
		call    func(*graphQueryAdapter) ([]fusion.Evidence, error)
	}{
		{
			name: "batch", subject: "graph.query.batch",
			payload: graph.EntityBatchResponse{Entities: []graph.EntityState{{ID: entityID}}},
			call: func(a *graphQueryAdapter) ([]fusion.Evidence, error) {
				return a.EntityState(context.Background(), EntityStateArgs{EntityIDs: []string{entityID}}, "0", "batch", 5)
			},
		},
		{
			name: "relationships", subject: "graph.query.relationships",
			payload: []map[string]any{{"from_entity_id": entityID, "to_entity_id": neighborID, "edge_type": "robotics.sensor.reads"}},
			call: func(a *graphQueryAdapter) ([]fusion.Evidence, error) {
				return a.PredicateWalk(context.Background(), PredicateWalkArgs{Seeds: []string{entityID}}, "0", "relationships", 5)
			},
		},
		{
			name: "temporal", subject: "graph.query.temporal",
			payload: []map[string]any{{"id": entityID, "type": "observation"}},
			call: func(a *graphQueryAdapter) ([]fusion.Evidence, error) {
				return a.TemporalRange(context.Background(), TemporalRangeArgs{Start: "2026-08-10T00:00:00Z", End: "2026-08-10T01:00:00Z"}, "0", "temporal", 5)
			},
		},
		{
			name: "searchGraph", subject: "graph.query.searchGraph",
			payload: map[string]any{"entity_digests": []map[string]any{{"id": entityID, "relevance": 0.81}}},
			call: func(a *graphQueryAdapter) ([]fusion.Evidence, error) {
				return a.BM25(context.Background(), BM25Args{Query: "drone", Limit: 5}, "1", "searchGraph", 5)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bare, err := json.Marshal(tc.payload)
			if err != nil {
				t.Fatal(err)
			}
			enveloped, err := json.Marshal(graph.NewQueryResponse(json.RawMessage(bare)))
			if err != nil {
				t.Fatal(err)
			}
			var results [][]fusion.Evidence
			for _, response := range [][]byte{bare, enveloped} {
				a := newGraphQueryAdapter(nil, graphQueryAdapterSubjects{
					batch: tc.subject, relationships: tc.subject, temporal: tc.subject, searchGraph: tc.subject,
				}, time.Second, nil)
				a.client = &sliceEExecuteRequester{response: response}
				got, callErr := tc.call(a)
				if callErr != nil {
					t.Fatalf("call: %v", callErr)
				}
				results = append(results, got)
			}
			if !reflect.DeepEqual(results[0], results[1]) {
				t.Fatalf("bare result %+v != enveloped result %+v", results[0], results[1])
			}
			if len(results[0]) != 1 {
				t.Fatalf("evidence count = %d, want 1", len(results[0]))
			}
		})
	}
}

func TestSliceEExecuteUnwrapsExactlyOneLayer(t *testing.T) {
	payload := json.RawMessage(`{"entities":[{"id":"acme.ops.robotics.gcs.drone.001"}]}`)
	inner, err := json.Marshal(graph.NewQueryResponse(payload))
	if err != nil {
		t.Fatal(err)
	}
	outer, err := json.Marshal(graph.NewQueryResponse(json.RawMessage(inner)))
	if err != nil {
		t.Fatal(err)
	}
	a := newGraphQueryAdapter(nil, graphQueryAdapterSubjects{}, time.Second, nil)
	a.client = &sliceEExecuteRequester{response: outer}
	got, err := a.request(context.Background(), "graph.query.batch", map[string]any{"ids": []string{"acme.ops.robotics.gcs.drone.001"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, inner) {
		t.Fatalf("adapter must remove exactly one envelope\n got: %s\nwant: %s", got, inner)
	}
}
