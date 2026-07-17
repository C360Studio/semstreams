package graphindex

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/revlag"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type lifecycleSpy struct{ calls atomic.Int64 }

func (s *lifecycleSpy) ReportStage(context.Context, string) error { s.calls.Add(1); return nil }
func (s *lifecycleSpy) ReportCycleStart(context.Context) error    { s.calls.Add(1); return nil }
func (s *lifecycleSpy) ReportCycleComplete(context.Context) error { s.calls.Add(1); return nil }
func (s *lifecycleSpy) ReportCycleError(context.Context, error) error {
	s.calls.Add(1)
	return nil
}

func TestAuthoritativeCandidatePreflightIsAtomicAcrossAllStores(t *testing.T) {
	t.Parallel()
	comp := createTestComponentWithMockKV(t)
	spy := &lifecycleSpy{}
	comp.lifecycleReporter = spy
	var ioCalls atomic.Int64
	for _, bucket := range []*mockKVBucket{
		outgoingMock(comp), incomingMock(comp), predicateMock(comp), nameMock(comp), contextMock(comp), aliasMock(comp),
	} {
		bucket.putFunc = func(context.Context, string, []byte) (uint64, error) { ioCalls.Add(1); return 1, nil }
		bucket.deleteFunc = func(context.Context, string, ...jetstream.KVDeleteOpt) error { ioCalls.Add(1); return nil }
		bucket.listFilteredFunc = func(context.Context, ...string) (jetstream.KeyLister, error) {
			ioCalls.Add(1)
			return newMockKeyLister(nil), nil
		}
	}
	entityID := "acme.ops.robotics.gcs.drone.001"
	state := graph.EntityState{ID: entityID, Triples: []message.Triple{
		{Subject: entityID, Predicate: "robotics.assigned.mission", Object: "acme.ops.robotics.gcs.mission.001"},
		{Subject: entityID, Predicate: "dc.terms.title", Object: "Valid Name", Context: "valid.context"},
		{Subject: entityID, Predicate: "core.identity.alias", Object: "invalid.*.alias"},
	}}
	data, err := json.Marshal(state)
	require.NoError(t, err)
	err = comp.processEntityUpdateFromData(context.Background(), entityID, data)
	require.Error(t, err)
	assert.Zero(t, ioCalls.Load(), "late-invalid candidate must not touch any derived store")
	assert.Zero(t, spy.calls.Load(), "late-invalid candidate must not report lifecycle progress")
	assert.Zero(t, comp.messagesProcessed)
	assert.Zero(t, comp.reindexTotal)
}

func TestAuthoritativeIdentityMismatchPoisonsBeforeIndexIOAndWithholdsWatermark(t *testing.T) {
	t.Parallel()
	comp := createTestComponentWithMockKV(t)
	comp.watermark = revlag.New()
	keyID := "acme.ops.robotics.gcs.drone.001"
	valueID := "acme.ops.robotics.gcs.drone.002"
	data := entityStateData(t, valueID, "acme.ops.robotics.gcs.mission.001")
	comp.entityStatesBucket = &mockKVBucket{getFunc: func(context.Context, string) (jetstream.KeyValueEntry, error) {
		return &orderedTestEntry{key: keyID, value: data, revision: 7}, nil
	}}
	var puts atomic.Int64
	for _, bucket := range []*mockKVBucket{
		outgoingMock(comp), incomingMock(comp), predicateMock(comp), nameMock(comp), contextMock(comp),
	} {
		bucket.putFunc = func(context.Context, string, []byte) (uint64, error) {
			puts.Add(1)
			return 1, nil
		}
	}

	comp.watermark.Observe(7, keyID)
	comp.processEntityWork(context.Background(), entityIndexWork{entityID: keyID, completionRevision: 7})

	assert.Zero(t, puts.Load(), "identity mismatch must fail before derived-index I/O")
	assert.Equal(t, int64(1), comp.failedCount.Load())
	assert.Equal(t, uint64(6), comp.watermark.Indexed())
	status := comp.computeIndexStatus(context.Background())
	assert.Equal(t, graph.IndexStateResetRequired, status.State)
	assert.Equal(t, string(graph.GraphStateReasonNoncanonicalEntityID), status.Reason)
}

func TestDirectIndexPathsRejectMalformedInputsBeforeBucketIO(t *testing.T) {
	t.Parallel()
	t.Run("outgoing get", func(t *testing.T) {
		comp := createTestComponentWithMockKV(t)
		var gets atomic.Int64
		outgoingMock(comp).getFunc = func(context.Context, string) (jetstream.KeyValueEntry, error) {
			gets.Add(1)
			return nil, jetstream.ErrKeyNotFound
		}
		err := comp.UpdateOutgoingIndex(context.Background(), "bad", "also.bad", "robotics.assigned.mission")
		require.Error(t, err)
		assert.Zero(t, gets.Load())
	})

	t.Run("context lister", func(t *testing.T) {
		comp := createTestComponentWithMockKV(t)
		var lists atomic.Int64
		contextMock(comp).listFilteredFunc = func(context.Context, ...string) (jetstream.KeyLister, error) {
			lists.Add(1)
			return newMockKeyLister(nil), nil
		}
		err := comp.UpdateContextIndex(context.Background(), "bad", nil)
		require.Error(t, err)
		assert.Zero(t, lists.Load())
	})
}

func TestPublicReverseQueriesSkipNoncanonicalPredicateRows(t *testing.T) {
	t.Parallel()
	t.Run("incoming", func(t *testing.T) {
		comp := createTestComponentWithMockKV(t)
		targetID := "acme.ops.robotics.gcs.mission.001"
		sourceID := "acme.ops.robotics.gcs.drone.001"
		key := incomingIndexKey(targetID, sourceID, "legacy.five.token.predicate.row")
		incomingMock(comp).data[key] = incomingIndexMarker
		request, err := json.Marshal(map[string]string{"entity_id": targetID})
		require.NoError(t, err)
		data, err := comp.handleQueryIncomingNATS(context.Background(), request)
		require.NoError(t, err)
		var response graph.IncomingQueryResponse
		require.NoError(t, json.Unmarshal(data, &response))
		assert.Empty(t, response.Data.Relationships)
	})

	t.Run("name", func(t *testing.T) {
		comp := createTestComponentWithMockKV(t)
		name := "Poison"
		entityID := "acme.ops.robotics.gcs.drone.001"
		key := nameCompositeKey(nameIndexKey(name), entityID, "legacy.five.token.predicate.row")
		value, err := json.Marshal(nameCompositeValue{Name: name, Priority: 1})
		require.NoError(t, err)
		nameMock(comp).data[key] = value
		assert.Empty(t, queryByName(t, comp, name, 0))
	})
}
