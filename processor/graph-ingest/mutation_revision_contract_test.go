package graphingest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

const absentSubjectID = "c360.platform.robotics.mav1.drone.absent01"

func TestHandleCanonicalAppend_AbsentEntityIsExplicitNotFound(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	triple := dedupTriple(absentSubjectID)
	requestData, err := json.Marshal(graph.AppendTriplesRequest{Triples: []message.Triple{triple}})
	require.NoError(t, err)

	body, err := comp.handleCanonicalAppend(ctx, requestData)
	require.NoError(t, err, "an absent subject is an explicit subject result")

	var resp graph.AppendTriplesResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	require.Len(t, resp.Results, 1)
	assert.Equal(t, absentSubjectID, resp.Results[0].EntityID)
	assert.Equal(t, graph.MutationEntityNotFound, resp.Results[0].Outcome)
	assert.Zero(t, resp.Results[0].KVRevision)
	assert.Nil(t, resp.Results[0].Error)
}

func TestHandleCanonicalAppend_SuppressedIsUnchanged(t *testing.T) {
	comp, _ := seedDedupEntity(t, dedupSubject)
	ctx := context.Background()
	triple := dedupTriple(dedupSubject)

	requestData, err := json.Marshal(graph.AppendTriplesRequest{Triples: []message.Triple{triple}})
	require.NoError(t, err)
	_, err = comp.handleCanonicalAppend(ctx, requestData)
	require.NoError(t, err)

	body, err := comp.handleCanonicalAppend(ctx, requestData)
	require.NoError(t, err)
	var resp graph.AppendTriplesResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	require.Len(t, resp.Results, 1)
	assert.Equal(t, graph.MutationUnchanged, resp.Results[0].Outcome)
	assert.NotZero(t, resp.Results[0].KVRevision)
}

func TestHandleCanonicalAppend_ReportsItsOwnCASRevision(t *testing.T) {
	comp, bucket := createTestComponentWithMockKVBucket(t)
	ctx := context.Background()
	require.NoError(t, comp.CreateEntity(ctx, &graph.EntityState{
		ID:          dedupSubject,
		MessageType: testEntityType(), Triples: []message.Triple{}, Version: 1, UpdatedAt: time.Now(),
	}))

	triple := dedupTriple(dedupSubject)
	requestData, err := json.Marshal(graph.AppendTriplesRequest{Triples: []message.Triple{triple}})
	require.NoError(t, err)

	gets := 0
	bucket.getFunc = func(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
		gets++
		return mockGetPassthrough(bucket, key)
	}
	body, err := comp.handleCanonicalAppend(ctx, requestData)
	bucket.getFunc = nil
	require.NoError(t, err)
	var resp graph.AppendTriplesResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	require.Len(t, resp.Results, 1)
	assert.Equal(t, 1, gets, "the committed path performs only the CAS read")
	assert.NotZero(t, resp.Results[0].KVRevision)
	_, liveRevision := readDedupEntity(t, comp, dedupSubject)
	assert.Equal(t, liveRevision, resp.Results[0].KVRevision)
}

func mockGetPassthrough(bucket *mockKVBucket, key string) (jetstream.KeyValueEntry, error) {
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	if data, exists := bucket.data[key]; exists {
		return &mockKVEntry{data: data.value, revision: data.revision, key: key}, nil
	}
	return nil, jetstream.ErrKeyNotFound
}

// structObject has fields whose DECLARATION order (Zebra, Alpha) differs from
// their sorted-key order (Alpha, Zebra). encoding/json emits a struct in
// declaration order but a map in sorted-key order, so this value and the
// map[string]any it decodes back into from ENTITY_STATES produce different JSON
// unless the identity key normalizes.
type structObject struct {
	Zebra string `json:"zebra"`
	Alpha int    `json:"alpha"`
}

// Codex C3. A struct-valued Object keyed differently from its persisted
// map[string]any form, so replaying the same valid in-process triple appended
// it again on EVERY restart — advancing revisions and refiring watchers, which
// is precisely the corruption this change exists to eliminate. "Fails safe" was
// the wrong lens: a missed suppression is the failure the requirement forbids.
func TestAddLane_StructuredObjectReplayIsSuppressed(t *testing.T) {
	structured := func(subject string) message.Triple {
		tr := dedupTriple(subject)
		tr.Predicate = "robotics.payload.descriptor"
		tr.Object = structObject{Zebra: "z", Alpha: 1}
		return tr
	}

	t.Run("single add lane", func(t *testing.T) {
		comp, _ := seedDedupEntity(t, dedupSubject)
		ctx := context.Background()

		appendDedupTriple(ctx, t, comp, structured(dedupSubject))
		first, revAfterFirst := readDedupEntity(t, comp, dedupSubject)
		require.Equal(t, 1, countDedupTriples(first, "robotics.payload.descriptor"),
			"fixture sanity: the structured triple must have been stored")

		// The replay: the identical in-process value, submitted again against
		// the stored map[string]any form.
		appendDedupTriple(ctx, t, comp, structured(dedupSubject))

		after, revAfter := readDedupEntity(t, comp, dedupSubject)
		assert.Equal(t, revAfterFirst, revAfter,
			"a replayed structured object must not advance the revision")
		assert.Equal(t, 1, countDedupTriples(after, "robotics.payload.descriptor"),
			"cardinality must be unchanged across the replay")
	})

	t.Run("large-int scalar, single add lane", func(t *testing.T) {
		// int64 above float64's exact range: the raw value and its persisted
		// form encode differently unless the key normalizes, so a replay would
		// re-append on every restart.
		bigInt := func(subject string) message.Triple {
			tr := dedupTriple(subject)
			tr.Predicate = "robotics.payload.sequence"
			tr.Object = int64(9007199254740993)
			return tr
		}
		comp, _ := seedDedupEntity(t, dedupSubject)
		ctx := context.Background()

		appendDedupTriple(ctx, t, comp, bigInt(dedupSubject))
		first, revAfterFirst := readDedupEntity(t, comp, dedupSubject)
		require.Equal(t, 1, countDedupTriples(first, "robotics.payload.sequence"),
			"fixture sanity: the triple must have been stored")

		appendDedupTriple(ctx, t, comp, bigInt(dedupSubject))

		after, revAfter := readDedupEntity(t, comp, dedupSubject)
		assert.Equal(t, revAfterFirst, revAfter,
			"a replayed large-int scalar must not advance the revision")
		assert.Equal(t, 1, countDedupTriples(after, "robotics.payload.sequence"),
			"cardinality must be unchanged across the replay")
	})

	t.Run("batch add lane", func(t *testing.T) {
		comp, _ := seedDedupEntity(t, dedupSubject)
		ctx := context.Background()
		batch := []message.Triple{structured(dedupSubject)}

		result, err := comp.addTriplesLane(ctx, batch, dedupLaneAddBatch)
		require.NoError(t, err)
		require.Equal(t, 1, result.Written, "fixture sanity: the first batch must commit")
		_, revAfterFirst := readDedupEntity(t, comp, dedupSubject)

		result, err = comp.addTriplesLane(ctx, batch, dedupLaneAddBatch)
		require.NoError(t, err)
		assert.Empty(t, result.FailedSubjects)
		assert.Equal(t, 0, result.Written, "the replayed batch must write nothing")
		assert.Equal(t, 1, result.Deduplicated, "and must report the tuple as already present")

		after, revAfter := readDedupEntity(t, comp, dedupSubject)
		assert.Equal(t, revAfterFirst, revAfter,
			"a replayed structured object must not advance the revision")
		assert.Equal(t, 1, countDedupTriples(after, "robotics.payload.descriptor"),
			"cardinality must be unchanged across the replay")
	})
}
