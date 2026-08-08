package agenticloop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrajectoryResponseContainsNoCompletenessMachinery(t *testing.T) {
	bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
	loopID := "loop-observed-only"
	putTrajectoryReaderFact(t, bucket, loopID,
		trajectoryReaderFact(loopID, "attempt1", 1, agentic.TrajectoryKindLoopTerminal, agentic.TrajectoryPhaseTerminal, 1, 0))
	response, err := newTrajectoryReader(bucket).read(
		context.Background(), agentic.TrajectoryQueryRequest{LoopID: loopID}, 1<<20)
	require.NoError(t, err)
	encoded, err := json.Marshal(response)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"coverage":"observed"`)
	for _, forbidden := range []string{"counts_known", "seal", "manifest", "watermark", "checkpoint", "gap_count", "attempted_count", "recorded_count", `"complete"`, `"partial"`} {
		assert.NotContains(t, string(encoded), forbidden)
	}
}

func TestTrajectoryReaderUsesVisibleFactsAndCausalOrder(t *testing.T) {
	bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
	loopID := "loop-restarted"
	facts := []agentic.TrajectoryFactV1{
		trajectoryReaderFact(loopID, "attempt3", 3, agentic.TrajectoryKindLoopTerminal, agentic.TrajectoryPhaseTerminal, 2, 0),
		trajectoryReaderFact(loopID, "attempt2", 2, agentic.TrajectoryKindToolCompleted, agentic.TrajectoryPhaseToolResult, 1, 40),
		trajectoryReaderFact(loopID, "attempt1", 1, agentic.TrajectoryKindModelCompleted, agentic.TrajectoryPhaseModelResult, 1, 20),
		trajectoryReaderFact(loopID, "attempt4", 4, agentic.TrajectoryKindLoopTerminal, agentic.TrajectoryPhaseTerminal, 2, 0),
	}
	facts[1].CausalOrdinal = 1
	facts[1].TokensIn, facts[1].TokensOut = 3, 5
	facts[2].TokensIn, facts[2].TokensOut = 7, 11
	facts[0].Status = agentic.TrajectoryStatusFailed
	facts[3].Status = agentic.TrajectoryStatusCompleted
	for _, fact := range facts {
		putTrajectoryReaderFact(t, bucket, loopID, fact)
	}

	reader := newTrajectoryReader(bucket)
	response, err := reader.read(context.Background(), agentic.TrajectoryQueryRequest{LoopID: loopID}, 1<<20)
	require.NoError(t, err)

	assert.Equal(t, trajectoryCoverageObserved, response.Coverage)
	assert.True(t, response.TerminalObserved)
	require.Len(t, response.Facts, 4)
	assert.Equal(t, []string{"attempt1", "attempt2", "attempt3", "attempt4"}, []string{
		response.Facts[0].AttemptID,
		response.Facts[1].AttemptID,
		response.Facts[2].AttemptID,
		response.Facts[3].AttemptID,
	})
	assert.Equal(t, uint64(10), response.ObservedTotals.TokensIn)
	assert.Equal(t, uint64(16), response.ObservedTotals.TokensOut)
	assert.Equal(t, int64(60), response.ObservedTotals.ElapsedMS)
	assert.Equal(t, uint64(2), response.ObservedTotals.TerminalObservations)
	assert.Equal(t, uint64(1), response.ObservedTotals.FailedObservations)
	assert.Equal(t, uint64(1), response.ObservedTotals.CompletedObservations)
}

func TestTrajectoryReaderReportsTerminalAbsenceWithoutInference(t *testing.T) {
	bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
	loopID := "loop-with-complete-adjacent-state"
	putTrajectoryReaderFact(t, bucket, loopID,
		trajectoryReaderFact(loopID, "attempt1", 1, agentic.TrajectoryKindLoopStarted, agentic.TrajectoryPhaseLoopStart, 0, 0))

	response, err := newTrajectoryReader(bucket).read(
		context.Background(), agentic.TrajectoryQueryRequest{LoopID: loopID}, 1<<20)
	require.NoError(t, err)
	assert.False(t, response.TerminalObserved)
	assert.Equal(t, trajectoryCoverageObserved, response.Coverage)
}

func TestTrajectoryReaderReturnsEvidenceReferencesWithoutBodies(t *testing.T) {
	loopID := "loop-evidence"
	bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
	body := []byte(`{"schema_version":"v1","kind":"tool.completed","body":{"result":"full"}}`)
	fact := trajectoryReaderFact(loopID, "attempt1", 1,
		agentic.TrajectoryKindToolCompleted, agentic.TrajectoryPhaseToolResult, 1, 1)
	attachTrajectoryReaderEvidence(t, &fact, "objectstore", body)
	putTrajectoryReaderFact(t, bucket, loopID, fact)

	page, err := newTrajectoryReader(bucket).read(
		context.Background(), agentic.TrajectoryQueryRequest{LoopID: loopID}, 1<<20)
	require.NoError(t, err)
	require.Len(t, page.Facts, 1)
	require.NotNil(t, page.Facts[0].Evidence)
	assert.Equal(t, "objectstore", page.Facts[0].Evidence.StorageInstance)
	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "evidence_body")
	assert.NotContains(t, string(encoded), "evidence_status")
}

func TestTrajectoryReaderRejectsStoredEvidenceMetadataDriftWithoutHydration(t *testing.T) {
	loopID := "loop-invalid-evidence-metadata"
	body := []byte(`{"schema_version":"v1","kind":"tool.completed","body":{"result":"full"}}`)
	tests := []struct {
		name   string
		mutate func(*agentic.TrajectoryFactV1)
	}{
		{name: "wrong content key", mutate: func(fact *agentic.TrajectoryFactV1) {
			fact.Evidence.Key = agentic.TrajectoryEvidenceKeyPrefix + strings.Repeat("0", 64)
		}},
		{name: "wrong content type", mutate: func(fact *agentic.TrajectoryFactV1) {
			fact.Evidence.ContentType = "application/json"
		}},
		{name: "reference size mismatch", mutate: func(fact *agentic.TrajectoryFactV1) {
			fact.Evidence.Size++
		}},
		{name: "empty storage instance", mutate: func(fact *agentic.TrajectoryFactV1) {
			fact.Evidence.StorageInstance = ""
		}},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
			fact := trajectoryReaderFact(loopID, fmt.Sprintf("attempt%d", index+1), uint64(index+1),
				agentic.TrajectoryKindToolCompleted, agentic.TrajectoryPhaseToolResult, uint32(index+1), 1)
			attachTrajectoryReaderEvidence(t, &fact, "objectstore", body)
			test.mutate(&fact)
			key, err := agentic.TrajectoryFactKey(loopID, fact.AttemptID)
			require.NoError(t, err)
			encoded, err := json.Marshal(fact)
			require.NoError(t, err)
			bucket.values[key] = encoded

			_, err = newTrajectoryReader(bucket).read(
				context.Background(), agentic.TrajectoryQueryRequest{LoopID: loopID}, 1<<20)
			assert.Error(t, err)
		})
	}
}

func TestTrajectoryReaderRejectsEmptyAndMismatchedPrefixes(t *testing.T) {
	reader := newTrajectoryReader(&trajectoryTestBucket{values: make(map[string][]byte)})
	_, err := reader.read(context.Background(), agentic.TrajectoryQueryRequest{LoopID: "missing"}, 1<<20)
	assert.ErrorIs(t, err, errTrajectoryNotFound)

	bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
	fact := trajectoryReaderFact("different-loop", "attempt1", 1, agentic.TrajectoryKindLoopStarted, agentic.TrajectoryPhaseLoopStart, 0, 0)
	bytes, encodeErr := fact.CanonicalBytes()
	require.NoError(t, encodeErr)
	bucket.values[agentic.TrajectoryFactPrefix("requested-loop")+fact.AttemptID] = bytes
	_, err = newTrajectoryReader(bucket).read(
		context.Background(), agentic.TrajectoryQueryRequest{LoopID: "requested-loop"}, 1<<20)
	assert.Error(t, err)
}

func TestTrajectoryQueryHandlerServesKVWithoutManagerState(t *testing.T) {
	bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
	loopID := "loop-handler-restart"
	putTrajectoryReaderFact(t, bucket, loopID,
		trajectoryReaderFact(loopID, "attempt1", 1, agentic.TrajectoryKindLoopStarted, agentic.TrajectoryPhaseLoopStart, 0, 0))
	component := &Component{trajectoryReader: newTrajectoryReader(bucket)}

	encoded, err := component.handleTrajectoryQueryWithMaxPayload(
		context.Background(), []byte(`{"loopId":"loop-handler-restart"}`), 1<<20)
	require.NoError(t, err)
	var response agentic.TrajectoryPage
	require.NoError(t, json.Unmarshal(encoded, &response))
	assert.Equal(t, trajectoryCoverageObserved, response.Coverage)
	require.Len(t, response.Facts, 1)

	_, err = component.handleTrajectoryQueryWithMaxPayload(
		context.Background(), []byte(`{"loopId":"loop-handler-restart","limit":-1}`), 1<<20)
	require.Error(t, err)
}

func trajectoryReaderFact(
	loopID, attemptID string,
	attemptOrdinal uint64,
	kind agentic.TrajectoryKind,
	phase agentic.TrajectoryPhase,
	iteration uint32,
	elapsedMS int64,
) agentic.TrajectoryFactV1 {
	return agentic.TrajectoryFactV1{
		SchemaVersion:   agentic.TrajectorySchemaV1,
		LoopDigest:      agentic.TrajectoryLoopDigest(loopID),
		AttemptID:       attemptID,
		AttemptOrdinal:  attemptOrdinal,
		Kind:            kind,
		CausalIteration: iteration,
		CausalPhase:     phase,
		ObservedAt:      time.Unix(int64(attemptOrdinal), 0).UTC(),
		ElapsedMS:       elapsedMS,
		EvidenceCapture: agentic.TrajectoryEvidenceNone,
	}
}

func putTrajectoryReaderFact(t *testing.T, bucket *trajectoryTestBucket, loopID string, fact agentic.TrajectoryFactV1) {
	t.Helper()
	key, err := agentic.TrajectoryFactKey(loopID, fact.AttemptID)
	require.NoError(t, err)
	encoded, err := fact.CanonicalBytes()
	require.NoError(t, err)
	bucket.values[key] = encoded
}

func attachTrajectoryReaderEvidence(t *testing.T, fact *agentic.TrajectoryFactV1, instance string, body []byte) {
	t.Helper()
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	key := agentic.TrajectoryEvidenceKeyPrefix + digest
	fact.EvidenceDigest = digest
	fact.EvidenceSize = uint64(len(body))
	fact.EvidenceCapture = agentic.TrajectoryEvidenceStored
	fact.Evidence = evidenceReference(instance, key, len(body))
}
