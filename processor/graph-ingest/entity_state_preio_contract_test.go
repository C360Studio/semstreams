package graphingest

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
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

// entity-id-audit:classify intentional-malformed "bad" line=29 column=46 surface=go-triple-subject entity_id_invalid:arity malformed triple subject pre-I/O rejection fixture
// entity-id-audit:classify intentional-malformed "bad" line=32 column=11 surface=go-triple-reference entity_id_invalid:arity malformed triple reference pre-I/O rejection fixture

func TestMutationEntityIdentityRejectionPrecedesKVIO(t *testing.T) {
	validID := "acme.ops.test.system.widget.001"
	validPredicate := semantictest.Predicate(t, "test", "state", "value")
	badSubjectTriple := message.Triple{Subject: "bad", Predicate: semantictest.Predicate(t, "test", "state", "value")}
	badReferenceTriple := message.Triple{
		Subject: validID, Predicate: semantictest.Predicate(t, "test", "state", "value"),
		Object: "bad", Datatype: message.EntityReferenceDatatype,
	}
	badBatch := []message.Triple{
		{Subject: validID, Predicate: semantictest.Predicate(t, "test", "state", "value")},
		{Subject: "bad", Predicate: semantictest.Predicate(t, "test", "state", "value")},
	}
	emptySubjectEntity := &graph.EntityState{
		ID:      validID,
		Triples: []message.Triple{{Subject: "", Predicate: semantictest.Predicate(t, "test", "state", "value")}},
	}

	tests := []struct {
		name string
		run  func(*Component) error
	}{
		{
			name: "add triple malformed subject",
			run: func(component *Component) error {
				return component.AddTriple(context.Background(), badSubjectTriple)
			},
		},
		{
			name: "add triple malformed explicit reference",
			run: func(component *Component) error {
				return component.AddTriple(context.Background(), badReferenceTriple)
			},
		},
		{
			name: "add triples rejects whole batch",
			run: func(component *Component) error {
				_, _, err := component.AddTriples(context.Background(), badBatch)
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
			// gh#562 write-cost: pins MergeEntity's kept entity-ID-only
			// preflight — the ID is the CAS key and the hierarchy-probe key,
			// so a malformed root ID must reject before any KV I/O even
			// though the full candidate pass moved to the write gate.
			name: "merge entity malformed root ID",
			run: func(component *Component) error {
				return component.MergeEntity(context.Background(), &graph.EntityState{
					ID:      "bad",
					Triples: []message.Triple{{Subject: validID, Predicate: validPredicate}},
				})
			},
		},
		{
			name: "direct create empty subject",
			run: func(component *Component) error {
				return component.CreateEntity(context.Background(), emptySubjectEntity)
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
	createRequest := graph.CreateEntityRequest{
		Entity: &graph.EntityState{ID: validID, Triples: []message.Triple{{
			Subject: "", Predicate: semantictest.Predicate(t, "test", "state", "value"),
		}}},
	}
	createWithTriplesRequest := graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{ID: validID},
		Triples: []message.Triple{{
			Subject: validID, Predicate: semantictest.Predicate(t, "test", "state", "value"), Object: 42, Datatype: message.EntityReferenceDatatype,
		}},
	}
	updateRequest := graph.UpdateEntityRequest{
		Entity: &graph.EntityState{ID: validID, Triples: []message.Triple{{
			Subject: "bad", Predicate: semantictest.Predicate(t, "test", "state", "value"),
		}}},
	}
	updateWithTriplesRequest := graph.UpdateEntityWithTriplesRequest{
		Entity: &graph.EntityState{ID: validID},
		AddTriples: []message.Triple{{
			Subject: "", Predicate: semantictest.Predicate(t, "test", "state", "value"),
		}},
	}

	tests := []struct {
		name string
		run  func(*Component) ([]byte, error)
	}{
		{
			name: "create",
			run: func(component *Component) ([]byte, error) {
				return component.handleEntityCreate(context.Background(), mutationRequestJSON(t, createRequest))
			},
		},
		{
			name: "create with triples explicit reference",
			run: func(component *Component) ([]byte, error) {
				return component.handleEntityCreateWithTriples(context.Background(), mutationRequestJSON(t, createWithTriplesRequest))
			},
		},
		{
			name: "update",
			run: func(component *Component) ([]byte, error) {
				return component.handleEntityUpdate(context.Background(), mutationRequestJSON(t, updateRequest))
			},
		},
		{
			name: "update with triples",
			run: func(component *Component) ([]byte, error) {
				return component.handleEntityUpdateWithTriples(context.Background(), mutationRequestJSON(t, updateWithTriplesRequest))
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
