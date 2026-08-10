package researchclassify

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
)

type sliceEClassifyRequester struct {
	response []byte
}

func (r *sliceEClassifyRequester) RequestClassified(context.Context, string, []byte, time.Duration) ([]byte, error) {
	return r.response, nil
}

func TestSliceEClassifyPreservesFullEntityOnlyResults(t *testing.T) {
	entities := []*graph.EntityState{
		{ID: "acme.ops.robotics.gcs.drone.001"},
		{ID: "acme.ops.robotics.gcs.sensor.002"},
		{ID: "acme.ops.robotics.gcs.vehicle.003"},
	}
	payload, err := json.Marshal(map[string]any{"entities": entities, "count": len(entities)})
	if err != nil {
		t.Fatal(err)
	}
	enveloped, err := json.Marshal(graph.NewQueryResponse(json.RawMessage(payload)))
	if err != nil {
		t.Fatal(err)
	}

	var results []CandidateSet
	for _, response := range [][]byte{payload, enveloped} {
		retriever := newSearchGraphRetriever(nil, "graph.query.searchGraph", time.Second)
		retriever.client = &sliceEClassifyRequester{response: response}
		got, fetchErr := retriever.FetchCandidates(context.Background(), "drone hover anomalies", nil, 2)
		if fetchErr != nil {
			t.Fatalf("FetchCandidates: %v", fetchErr)
		}
		results = append(results, got)
	}
	if len(results[0].Candidates) != 2 || len(results[1].Candidates) != 2 {
		t.Fatalf("bare/enveloped candidate counts = %d/%d, want 2/2 from the ordered full-entity representation",
			len(results[0].Candidates), len(results[1].Candidates))
	}
	if results[0].Candidates[0] != results[1].Candidates[0] || results[0].Candidates[1] != results[1].Candidates[1] {
		t.Fatalf("bare candidates %+v != enveloped candidates %+v", results[0].Candidates, results[1].Candidates)
	}
	got := results[0]
	if got.Candidates[0].EntityID != entities[0].ID || got.Candidates[1].EntityID != entities[1].ID {
		t.Fatalf("candidate order = [%s %s], want [%s %s]",
			got.Candidates[0].EntityID, got.Candidates[1].EntityID, entities[0].ID, entities[1].ID)
	}
	if got.Candidates[0].Type != "drone" || got.Candidates[1].Type != "sensor" {
		t.Fatalf("candidate types = [%s %s], want [drone sensor]",
			got.Candidates[0].Type, got.Candidates[1].Type)
	}
	if got.Candidates[0].Label != "" || got.Candidates[0].Relevance != 0 || got.Candidates[0].SnippetText != "" {
		t.Fatalf("full-entity projection invented unsupported facts: %+v", got.Candidates[0])
	}
}

func TestSliceEClassifyUnwrapsExactlyOneLayer(t *testing.T) {
	payload := json.RawMessage(`{"entities":[{"id":"acme.ops.robotics.gcs.drone.001"}],"count":1}`)
	inner, err := json.Marshal(graph.NewQueryResponse(payload))
	if err != nil {
		t.Fatal(err)
	}
	outer, err := json.Marshal(graph.NewQueryResponse(json.RawMessage(inner)))
	if err != nil {
		t.Fatal(err)
	}
	retriever := newSearchGraphRetriever(nil, "graph.query.searchGraph", time.Second)
	retriever.client = &sliceEClassifyRequester{response: outer}

	got, err := retriever.FetchCandidates(context.Background(), "drone", nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Candidates) != 0 {
		t.Fatalf("double envelope candidates = %d, want 0 after exactly one unwrap", len(got.Candidates))
	}
}

func TestSliceEClassifyBareAndEnvelopedRepliesAgree(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"entity_digests": []map[string]any{{
			"id": "acme.ops.robotics.gcs.drone.001", "type": "drone", "label": "Scout", "relevance": 0.91,
		}},
		"count": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	enveloped, err := json.Marshal(graph.NewQueryResponse(json.RawMessage(payload)))
	if err != nil {
		t.Fatal(err)
	}

	var results []CandidateSet
	for _, response := range [][]byte{payload, enveloped} {
		retriever := newSearchGraphRetriever(nil, "graph.query.searchGraph", time.Second)
		retriever.client = &sliceEClassifyRequester{response: response}
		got, fetchErr := retriever.FetchCandidates(context.Background(), "drone", nil, 5)
		if fetchErr != nil {
			t.Fatalf("FetchCandidates: %v", fetchErr)
		}
		results = append(results, got)
	}
	if len(results[0].Candidates) != 1 || len(results[1].Candidates) != 1 {
		t.Fatalf("bare/enveloped candidate counts = %d/%d, want 1/1",
			len(results[0].Candidates), len(results[1].Candidates))
	}
	if results[0].Candidates[0] != results[1].Candidates[0] {
		t.Fatalf("bare candidate %+v != enveloped candidate %+v", results[0].Candidates[0], results[1].Candidates[0])
	}
}
