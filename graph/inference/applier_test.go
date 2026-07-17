package inference

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/message"
)

// mockTripleAdder is a test double for TripleAdder.
type mockTripleAdder struct {
	addedTriples []message.Triple
	err          error
}

func (m *mockTripleAdder) AddTriple(_ context.Context, triple message.Triple) error {
	if m.err != nil {
		return m.err
	}
	m.addedTriples = append(m.addedTriples, triple)
	return nil
}

func TestDirectRelationshipApplier_ApplyRelationship_Success(t *testing.T) {
	mock := &mockTripleAdder{}
	applier := NewDirectRelationshipApplier(mock, slog.Default())

	suggestion := &RelationshipSuggestion{
		FromEntity: "c360.p1.robotics.sys1.drone.001",
		ToEntity:   "c360.p1.robotics.sys1.drone.002",
		Predicate:  "inference.related-to.high",
		Confidence: 0.85,
		Reasoning:  "high semantic similarity",
	}

	err := applier.ApplyRelationship(context.Background(), suggestion)

	require.NoError(t, err)
	require.Len(t, mock.addedTriples, 1)

	triple := mock.addedTriples[0]
	assert.Equal(t, suggestion.FromEntity, triple.Subject)
	assert.Equal(t, suggestion.Predicate, triple.Predicate)
	assert.Equal(t, suggestion.ToEntity, triple.Object)
	assert.Equal(t, "inference.structural", triple.Source)
	assert.Equal(t, suggestion.Confidence, triple.Confidence)
	assert.Equal(t, "auto-applied", triple.Context)
	assert.False(t, triple.Timestamp.IsZero())
}

func TestDirectRelationshipApplier_ApplyRelationship_NilSuggestion(t *testing.T) {
	mock := &mockTripleAdder{}
	applier := NewDirectRelationshipApplier(mock, slog.Default())

	err := applier.ApplyRelationship(context.Background(), nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "suggestion is nil")
	assert.Empty(t, mock.addedTriples)
}

func TestDirectRelationshipApplier_ApplyRelationship_EmptyFromEntity(t *testing.T) {
	mock := &mockTripleAdder{}
	applier := NewDirectRelationshipApplier(mock, slog.Default())

	suggestion := &RelationshipSuggestion{
		FromEntity: "",
		ToEntity:   "c360.p1.robotics.sys1.drone.002",
		Predicate:  "inference.related-to.high",
		Confidence: 0.85,
	}

	err := applier.ApplyRelationship(context.Background(), suggestion)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "from_entity and to_entity are required")
	assert.Empty(t, mock.addedTriples)
}

func TestDirectRelationshipApplier_ApplyRelationship_EmptyToEntity(t *testing.T) {
	mock := &mockTripleAdder{}
	applier := NewDirectRelationshipApplier(mock, slog.Default())

	suggestion := &RelationshipSuggestion{
		FromEntity: "c360.p1.robotics.sys1.drone.001",
		ToEntity:   "",
		Predicate:  "inference.related-to.high",
		Confidence: 0.85,
	}

	err := applier.ApplyRelationship(context.Background(), suggestion)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "from_entity and to_entity are required")
	assert.Empty(t, mock.addedTriples)
}

func TestDirectRelationshipApplier_ApplyRelationship_EmptyPredicate(t *testing.T) {
	mock := &mockTripleAdder{}
	applier := NewDirectRelationshipApplier(mock, slog.Default())

	suggestion := &RelationshipSuggestion{
		FromEntity: "c360.p1.robotics.sys1.drone.001",
		ToEntity:   "c360.p1.robotics.sys1.drone.002",
		Predicate:  "", // predicate-audit:invalid {"kind":"stored-predicate","value":"","reason":"empty"}
		Confidence: 0.85,
	}

	err := applier.ApplyRelationship(context.Background(), suggestion)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "predicate is required")
	assert.Empty(t, mock.addedTriples)
}

func TestDirectRelationshipApplier_ApplyRelationship_AdderError(t *testing.T) {
	expectedErr := errors.New("storage failure")
	mock := &mockTripleAdder{err: expectedErr}
	applier := NewDirectRelationshipApplier(mock, slog.Default())

	suggestion := &RelationshipSuggestion{
		FromEntity: "c360.p1.robotics.sys1.drone.001",
		ToEntity:   "c360.p1.robotics.sys1.drone.002",
		Predicate:  "inference.related-to.high",
		Confidence: 0.85,
	}

	err := applier.ApplyRelationship(context.Background(), suggestion)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add triple")
	assert.ErrorIs(t, err, expectedErr)
}

func TestDirectRelationshipApplier_NilLogger(t *testing.T) {
	mock := &mockTripleAdder{}
	applier := NewDirectRelationshipApplier(mock, nil)

	// Should not panic with nil logger
	require.NotNil(t, applier)
	require.NotNil(t, applier.logger)
}
