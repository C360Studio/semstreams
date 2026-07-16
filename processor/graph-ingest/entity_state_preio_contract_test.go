package graphingest

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func mutationRequestJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}

func TestMutationEntityIdentityRejectionPrecedesKVIO(t *testing.T) {
	validID := "acme.ops.test.system.widget.001"
	validPredicate := "test.state.value"

	tests := []struct {
		name string
		run  func(*Component) error
	}{
		{
			name: "add triple malformed subject",
			run: func(component *Component) error {
				return component.AddTriple(context.Background(), message.Triple{
					Subject: "bad", Predicate: validPredicate,
				})
			},
		},
		{
			name: "add triple malformed explicit reference",
			run: func(component *Component) error {
				return component.AddTriple(context.Background(), message.Triple{
					Subject: validID, Predicate: validPredicate,
					Object: "bad", Datatype: message.EntityReferenceDatatype,
				})
			},
		},
		{
			name: "add triples rejects whole batch",
			run: func(component *Component) error {
				_, _, err := component.AddTriples(context.Background(), []message.Triple{
					{Subject: validID, Predicate: validPredicate},
					{Subject: "bad", Predicate: validPredicate},
				})
				return err
			},
		},
		{
			name: "remove triple malformed subject",
			run: func(component *Component) error {
				return component.RemoveTriple(context.Background(), "bad", validPredicate)
			},
		},
		{
			name: "direct create empty subject",
			run: func(component *Component) error {
				return component.CreateEntity(context.Background(), &graph.EntityState{
					ID:      validID,
					Triples: []message.Triple{{Subject: "", Predicate: validPredicate}},
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			component, bucket := createTestComponentWithMockKVBucket(t)
			var calls atomic.Int32
			bucket.getFunc = func(context.Context, string) (jetstream.KeyValueEntry, error) {
				calls.Add(1)
				return nil, jetstream.ErrKeyNotFound
			}
			bucket.putFunc = func(context.Context, string, []byte) (uint64, error) {
				calls.Add(1)
				return 1, nil
			}

			err := tt.run(component)
			require.Error(t, err)
			require.Equal(t, int32(0), calls.Load(), "invalid input reached KV")
		})
	}
}

func TestEntityDeleteHandlerValidatesBeforeExistenceRead(t *testing.T) {
	component, bucket := createTestComponentWithMockKVBucket(t)
	var calls atomic.Int32
	bucket.getFunc = func(context.Context, string) (jetstream.KeyValueEntry, error) {
		calls.Add(1)
		return nil, jetstream.ErrKeyNotFound
	}
	bucket.deleteFunc = func(context.Context, string, ...jetstream.KVDeleteOpt) error {
		calls.Add(1)
		return nil
	}

	_, err := component.handleEntityDelete(context.Background(), []byte(`{"entity_id":"bad"}`))
	require.Error(t, err)
	require.Equal(t, int32(0), calls.Load(), "invalid delete reached KV")
}

func TestEntityMutationHandlersValidateCompleteCandidateBeforeKVIO(t *testing.T) {
	validID := "acme.ops.test.system.widget.001"
	validPredicate := "test.state.value"

	tests := []struct {
		name string
		run  func(*Component) ([]byte, error)
	}{
		{
			name: "create",
			run: func(component *Component) ([]byte, error) {
				return component.handleEntityCreate(context.Background(), mutationRequestJSON(t, graph.CreateEntityRequest{
					Entity: &graph.EntityState{ID: validID, Triples: []message.Triple{{Subject: "", Predicate: validPredicate}}},
				}))
			},
		},
		{
			name: "create with triples explicit reference",
			run: func(component *Component) ([]byte, error) {
				return component.handleEntityCreateWithTriples(context.Background(), mutationRequestJSON(t, graph.CreateEntityWithTriplesRequest{
					Entity: &graph.EntityState{ID: validID},
					Triples: []message.Triple{{
						Subject: validID, Predicate: validPredicate, Object: 42, Datatype: message.EntityReferenceDatatype,
					}},
				}))
			},
		},
		{
			name: "update",
			run: func(component *Component) ([]byte, error) {
				return component.handleEntityUpdate(context.Background(), mutationRequestJSON(t, graph.UpdateEntityRequest{
					Entity: &graph.EntityState{ID: validID, Triples: []message.Triple{{Subject: "bad", Predicate: validPredicate}}},
				}))
			},
		},
		{
			name: "update with triples",
			run: func(component *Component) ([]byte, error) {
				return component.handleEntityUpdateWithTriples(context.Background(), mutationRequestJSON(t, graph.UpdateEntityWithTriplesRequest{
					Entity:     &graph.EntityState{ID: validID},
					AddTriples: []message.Triple{{Subject: "", Predicate: validPredicate}},
				}))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			component, bucket := createTestComponentWithMockKVBucket(t)
			var calls atomic.Int32
			bucket.getFunc = func(context.Context, string) (jetstream.KeyValueEntry, error) {
				calls.Add(1)
				return nil, jetstream.ErrKeyNotFound
			}
			bucket.putFunc = func(context.Context, string, []byte) (uint64, error) {
				calls.Add(1)
				return 1, nil
			}
			bucket.createFunc = func(context.Context, string, []byte, ...jetstream.KVCreateOpt) (uint64, error) {
				calls.Add(1)
				return 1, nil
			}

			_, err := tt.run(component)
			require.Error(t, err)
			require.Equal(t, int32(0), calls.Load(), "invalid handler input reached KV")
		})
	}
}
