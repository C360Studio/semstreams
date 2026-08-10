package fusionnats

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/fusion"
)

func TestSliceEFusionEntityUsesExactEntityWire(t *testing.T) {
	id := "acme.ops.robotics.gcs.drone.001"
	exact := graph.ExactEntity{Entity: &graph.EntityState{ID: id, Triples: []message.Triple{{
		Subject: id, Predicate: "dc.terms.title", Object: "Scout",
	}}}, KVRevision: 42}
	c := New(&fakeRequester{resp: mustJSON(t, exact)}, time.Second)

	entity, err := c.Entity(context.Background(), id)
	if err != nil {
		t.Fatalf("Entity: %v", err)
	}
	if entity == nil || entity.ID != id || entity.First("dc.terms.title") != "Scout" {
		t.Fatalf("entity projection = %+v", entity)
	}
}

func TestSliceEFusionEntityRejectsInvalidExactEvidence(t *testing.T) {
	id := "acme.ops.robotics.gcs.drone.001"
	tests := []struct {
		name string
		wire graph.ExactEntity
		want string
	}{
		{name: "nil entity", wire: graph.ExactEntity{KVRevision: 1}, want: "entity"},
		{name: "zero revision", wire: graph.ExactEntity{Entity: &graph.EntityState{ID: id}}, want: "revision"},
		{name: "requested ID mismatch", wire: graph.ExactEntity{Entity: &graph.EntityState{ID: "acme.ops.robotics.gcs.sensor.002"}, KVRevision: 1}, want: "mismatch"},
		{name: "poisoned entity", wire: graph.ExactEntity{Entity: &graph.EntityState{ID: "bad"}, KVRevision: 1}, want: "validate"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := New(&fakeRequester{resp: mustJSON(t, tc.wire)}, time.Second)
			_, err := c.Entity(context.Background(), id)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tc.want) {
				t.Fatalf("error = %v, want text containing %q", err, tc.want)
			}
		})
	}
}

func TestSliceEFusionSixSubjectsAcceptBareAndEnvelopedReplies(t *testing.T) {
	id := "acme.ops.robotics.gcs.drone.001"
	neighbor := "acme.ops.robotics.gcs.sensor.002"
	tests := []struct {
		name    string
		payload any
		call    func(*Client) (any, error)
	}{
		{name: "byName", payload: graph.NameData{Matches: []graph.NameMatch{{EntityID: id, MatchedName: "Scout"}}}, call: func(c *Client) (any, error) { return c.Names(context.Background(), "Sco", 5) }},
		{name: "prefix", payload: graph.PrefixQueryResponse{Entities: []graph.EntityState{{ID: id}}}, call: func(c *Client) (any, error) {
			return c.Resolve(context.Background(), fusion.ResolveQuery{Query: "acme.ops", Mode: fusion.ResolveModePrefix, Limit: 5})
		}},
		{name: "semantic", payload: map[string]any{"results": []map[string]any{{"entity_id": id, "similarity": 0.9}}}, call: func(c *Client) (any, error) {
			return c.Resolve(context.Background(), fusion.ResolveQuery{Query: "scout", Mode: fusion.ResolveModeNL, Limit: 5})
		}},
		{name: "entity", payload: graph.ExactEntity{Entity: &graph.EntityState{ID: id}, KVRevision: 3}, call: func(c *Client) (any, error) { return c.Entity(context.Background(), id) }},
		{name: "batch", payload: graph.EntityBatchResponse{Entities: []graph.EntityState{{ID: id}}}, call: func(c *Client) (any, error) { return c.Entities(context.Background(), []string{id}) }},
		{name: "relationships", payload: []map[string]any{{"from_entity_id": id, "to_entity_id": neighbor, "edge_type": "robotics.sensor.reads"}}, call: func(c *Client) (any, error) { return c.Neighbors(context.Background(), id, nil, fusion.Outgoing) }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bare := mustJSON(t, tc.payload)
			enveloped := mustJSON(t, graph.NewQueryResponse(json.RawMessage(bare)))
			results := make([]any, 0, 2)
			for _, response := range [][]byte{bare, enveloped} {
				got, err := tc.call(New(&fakeRequester{resp: response}, time.Second))
				if err != nil {
					t.Fatalf("call: %v", err)
				}
				results = append(results, got)
			}
			if !reflect.DeepEqual(results[0], results[1]) {
				t.Fatalf("bare result %#v != enveloped result %#v", results[0], results[1])
			}
		})
	}
}

func TestSliceEFusionUnwrapsExactlyOneLayer(t *testing.T) {
	inner := mustJSON(t, graph.NewQueryResponse(map[string]any{"results": []any{}}))
	outer := mustJSON(t, graph.NewQueryResponse(json.RawMessage(inner)))
	c := New(&fakeRequester{resp: outer}, time.Second)
	got, err := c.request(context.Background(), subjectSemantic, map[string]any{"query": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, inner) {
		t.Fatalf("adapter must remove exactly one envelope\n got: %s\nwant: %s", got, inner)
	}
}
