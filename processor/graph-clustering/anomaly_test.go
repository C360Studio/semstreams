package graphclustering

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/inference"
)

// Tests for kvRelationshipQuerier
// Uses mockKVBucket from component_test.go

func TestKVRelationshipQuerier_GetOutgoingRelationships_PreservesPredicates(t *testing.T) {
	outgoingBucket := newMockKVBucket()
	incomingBucket := newMockKVBucket()

	// Store relationships with predicates
	relationships := []relationshipEntry{
		{Predicate: "org.employment.works-for", ToEntityID: "test.graph.cluster.anomaly.entity.b"},
		{Predicate: "org.membership.member-of", ToEntityID: "test.graph.cluster.anomaly.entity.c"},
	}
	data, err := json.Marshal(relationships)
	if err != nil {
		t.Fatalf("failed to marshal relationships: %v", err)
	}
	if _, err := outgoingBucket.Put(context.Background(), "test.graph.cluster.anomaly.entity.a", data); err != nil {
		t.Fatalf("failed to put data: %v", err)
	}

	querier := newKVRelationshipQuerier(outgoingBucket, incomingBucket, nil)

	result, err := querier.GetOutgoingRelationships(context.Background(), "test.graph.cluster.anomaly.entity.a")
	if err != nil {
		t.Fatalf("GetOutgoingRelationships failed: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 relationships, got %d", len(result))
	}

	// Verify predicates are preserved
	if result[0].Predicate != "org.employment.works-for" {
		t.Errorf("expected predicate 'org.employment.works-for', got %s", result[0].Predicate)
	}
	if result[0].ToEntityID != "test.graph.cluster.anomaly.entity.b" {
		t.Errorf("expected ToEntityID 'test.graph.cluster.anomaly.entity.b', got %s", result[0].ToEntityID)
	}
	if result[0].FromEntityID != "test.graph.cluster.anomaly.entity.a" {
		t.Errorf("expected canonical source entity ID, got %s", result[0].FromEntityID)
	}

	if result[1].Predicate != "org.membership.member-of" {
		t.Errorf("expected predicate 'org.membership.member-of', got %s", result[1].Predicate)
	}
	if result[1].ToEntityID != "test.graph.cluster.anomaly.entity.c" {
		t.Errorf("expected ToEntityID 'test.graph.cluster.anomaly.entity.c', got %s", result[1].ToEntityID)
	}
}

func TestKVRelationshipQuerier_GetIncomingRelationships_PreservesPredicates(t *testing.T) {
	outgoingBucket := newMockKVBucket()
	incomingBucket := newMockKVBucket()

	// Store incoming relationships in the composite-key sharded format (gh#474):
	// one key per edge — "targetID.sourceID.hex(predicate)" with an empty marker value
	// (the predicate is hex-encoded, gh#474 P1a). Entity IDs must be valid 6-part
	// federated IDs for the parser to reconstruct them.
	targetID := "acme.ops.graph.test.entity.b"
	sourceA := "acme.ops.graph.test.entity.a"
	sourceC := "acme.ops.graph.test.entity.c"

	if _, err := incomingBucket.Put(context.Background(), targetID+"."+sourceA+"."+graph.EncodePredicateToken("org.employment.works-for"), []byte{}); err != nil {
		t.Fatalf("failed to put data: %v", err)
	}
	if _, err := incomingBucket.Put(context.Background(), targetID+"."+sourceC+"."+graph.EncodePredicateToken("org.reporting.reports-to"), []byte{}); err != nil {
		t.Fatalf("failed to put data: %v", err)
	}

	querier := newKVRelationshipQuerier(outgoingBucket, incomingBucket, nil)

	result, err := querier.GetIncomingRelationships(context.Background(), targetID)
	if err != nil {
		t.Fatalf("GetIncomingRelationships failed: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 relationships, got %d", len(result))
	}

	// Verify predicates and entity IDs are correctly reconstructed from composite keys.
	bySource := make(map[string]string)
	for _, r := range result {
		bySource[r.FromEntityID] = r.Predicate
		if r.ToEntityID != targetID {
			t.Errorf("expected ToEntityID %q, got %q", targetID, r.ToEntityID)
		}
	}
	if bySource[sourceA] != "org.employment.works-for" {
		t.Errorf("expected predicate 'org.employment.works-for' for source %s, got %q", sourceA, bySource[sourceA])
	}
	if bySource[sourceC] != "org.reporting.reports-to" {
		t.Errorf("expected predicate 'org.reporting.reports-to' for source %s, got %q", sourceC, bySource[sourceC])
	}
}

func TestKVRelationshipQuerier_GetOutgoingRelationships_NotFound(t *testing.T) {
	outgoingBucket := newMockKVBucket()
	incomingBucket := newMockKVBucket()

	querier := newKVRelationshipQuerier(outgoingBucket, incomingBucket, nil)

	result, err := querier.GetOutgoingRelationships(context.Background(), "test.graph.cluster.anomaly.entity.missing")
	if err != nil {
		t.Fatalf("expected no error for missing entity, got: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for missing entity, got: %v", result)
	}
}

func TestKVRelationshipQuerier_GetIncomingRelationships_NotFound(t *testing.T) {
	outgoingBucket := newMockKVBucket()
	incomingBucket := newMockKVBucket()

	querier := newKVRelationshipQuerier(outgoingBucket, incomingBucket, nil)

	result, err := querier.GetIncomingRelationships(context.Background(), "test.graph.cluster.anomaly.entity.missing")
	if err != nil {
		t.Fatalf("expected no error for missing entity, got: %v", err)
	}
	// The composite-key scan returns an empty slice (not nil) when no keys match.
	// Callers should check len(result) == 0, not result != nil.
	if len(result) != 0 {
		t.Errorf("expected empty result for missing entity, got: %v", result)
	}
}

func TestKVRelationshipQuerier_GetOutgoingRelationships_EmptyList(t *testing.T) {
	outgoingBucket := newMockKVBucket()
	incomingBucket := newMockKVBucket()

	// Store empty relationship list
	data, _ := json.Marshal([]relationshipEntry{})
	if _, err := outgoingBucket.Put(context.Background(), "test.graph.cluster.anomaly.entity.a", data); err != nil {
		t.Fatalf("failed to put data: %v", err)
	}

	querier := newKVRelationshipQuerier(outgoingBucket, incomingBucket, nil)

	result, err := querier.GetOutgoingRelationships(context.Background(), "test.graph.cluster.anomaly.entity.a")
	if err != nil {
		t.Fatalf("GetOutgoingRelationships failed: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 relationships, got %d", len(result))
	}
}

func TestKVRelationshipQuerier_GetOutgoingRelationships_InvalidJSON(t *testing.T) {
	outgoingBucket := newMockKVBucket()
	incomingBucket := newMockKVBucket()

	// Store invalid JSON
	if _, err := outgoingBucket.Put(context.Background(), "test.graph.cluster.anomaly.entity.a", []byte("not valid json")); err != nil {
		t.Fatalf("failed to put data: %v", err)
	}

	querier := newKVRelationshipQuerier(outgoingBucket, incomingBucket, nil)

	_, err := querier.GetOutgoingRelationships(context.Background(), "test.graph.cluster.anomaly.entity.a")
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestKVRelationshipQuerier_ImplementsInterface(_ *testing.T) {
	outgoingBucket := newMockKVBucket()
	incomingBucket := newMockKVBucket()

	querier := newKVRelationshipQuerier(outgoingBucket, incomingBucket, nil)

	// Verify it implements the interface
	var _ inference.RelationshipQuerier = querier
}

func TestGraphProviderAdapter_DoesNotPreservePredicates(t *testing.T) {
	// This test documents that graphProviderAdapter does NOT preserve predicates
	// It's kept for reference to show why kvRelationshipQuerier was needed

	mockProvider := &mockClusteringProvider{
		neighbors: map[string][]string{
			"test.graph.cluster.anomaly.entity.a": {"test.graph.cluster.anomaly.entity.b", "test.graph.cluster.anomaly.entity.c"},
		},
	}

	adapter := &graphProviderAdapter{provider: mockProvider}

	result, err := adapter.GetOutgoingRelationships(context.Background(), "test.graph.cluster.anomaly.entity.a")
	if err != nil {
		t.Fatalf("GetOutgoingRelationships failed: %v", err)
	}

	// graphProviderAdapter returns relationships WITHOUT predicates
	for _, rel := range result {
		if rel.Predicate != "" {
			t.Errorf("graphProviderAdapter should NOT have predicates, got: %s", rel.Predicate)
		}
	}
}

// mockClusteringProvider implements graph.Provider for testing graphProviderAdapter
type mockClusteringProvider struct {
	neighbors map[string][]string
}

func (m *mockClusteringProvider) GetNeighbors(_ context.Context, entityID string, _ string) ([]string, error) {
	return m.neighbors[entityID], nil
}

func (m *mockClusteringProvider) GetAllEntityIDs(_ context.Context) ([]string, error) {
	entities := make([]string, 0)
	for e := range m.neighbors {
		entities = append(entities, e)
	}
	return entities, nil
}

func (m *mockClusteringProvider) GetEdgeWeight(_ context.Context, _, _ string) (float64, error) {
	return 1.0, nil
}
