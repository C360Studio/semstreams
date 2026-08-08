package agenticloop

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrajectoryReaderPagesDeterministicallyWithPageLocalTruth(t *testing.T) {
	const loopID = "loop-paged"
	bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
	for i := 1; i <= 70; i++ {
		kind := agentic.TrajectoryKindModelCompleted
		phase := agentic.TrajectoryPhaseModelResult
		if i == 70 {
			kind = agentic.TrajectoryKindLoopTerminal
			phase = agentic.TrajectoryPhaseTerminal
		}
		fact := trajectoryReaderFact(loopID, trajectoryAttemptID(i), uint64(i), kind, phase, uint32(i), 1)
		fact.TokensIn = 1
		putTrajectoryReaderFact(t, bucket, loopID, fact)
	}

	reader := newTrajectoryReader(bucket)
	first, err := reader.read(context.Background(), agentic.TrajectoryQueryRequest{LoopID: loopID}, 1<<20)
	require.NoError(t, err)
	require.Len(t, first.Facts, 64)
	assert.NotEmpty(t, first.NextCursor)
	assert.Equal(t, uint64(64), first.ObservedTotals.Facts)
	assert.Equal(t, uint64(64), first.ObservedTotals.TokensIn)
	assert.False(t, first.TerminalObserved)
	assert.Equal(t, 70, bucket.getCalls, "result limit must not become a KV scan bound")

	second, err := reader.read(context.Background(), agentic.TrajectoryQueryRequest{
		LoopID: loopID,
		Cursor: first.NextCursor,
	}, 1<<20)
	require.NoError(t, err)
	require.Len(t, second.Facts, 6)
	assert.Empty(t, second.NextCursor)
	assert.Equal(t, uint64(6), second.ObservedTotals.Facts)
	assert.Equal(t, uint64(6), second.ObservedTotals.TokensIn)
	assert.Equal(t, uint64(1), second.ObservedTotals.TerminalObservations)
	assert.True(t, second.TerminalObserved)
	assert.Equal(t, trajectoryAttemptID(65), second.Facts[0].AttemptID)
	assert.Equal(t, 140, bucket.getCalls, "every page must validate the complete visible fact set")
}

func TestTrajectoryReaderRejectsPartialKeyListingOnCancellation(t *testing.T) {
	const loopID = "loop-partial-list"
	bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
	fact := trajectoryReaderFact(loopID, "attempt1", 1,
		agentic.TrajectoryKindLoopStarted, agentic.TrajectoryPhaseLoopStart, 0, 0)
	putTrajectoryReaderFact(t, bucket, loopID, fact)
	key, err := agentic.TrajectoryFactKey(loopID, fact.AttemptID)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	keys := make(chan string)
	bucket.listLister = &trajectoryTestLister{keys: keys}
	go func() {
		keys <- key
		cancel()
		close(keys)
	}()

	page, err := newTrajectoryReader(bucket).read(ctx, agentic.TrajectoryQueryRequest{LoopID: loopID}, 1<<20)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, page.Facts)
	assert.Equal(t, 0, bucket.getCalls, "partial listings must fail before any fact is read")
}

func TestTrajectoryReaderDeduplicatesListedKeysBeforeGet(t *testing.T) {
	const loopID = "loop-duplicate-list"
	bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
	fact := trajectoryReaderFact(loopID, "attempt1", 1,
		agentic.TrajectoryKindLoopStarted, agentic.TrajectoryPhaseLoopStart, 0, 0)
	putTrajectoryReaderFact(t, bucket, loopID, fact)
	key, err := agentic.TrajectoryFactKey(loopID, fact.AttemptID)
	require.NoError(t, err)

	keys := make(chan string, 2)
	keys <- key
	keys <- key
	close(keys)
	bucket.listLister = &trajectoryTestLister{keys: keys}

	page, err := newTrajectoryReader(bucket).read(
		context.Background(), agentic.TrajectoryQueryRequest{LoopID: loopID}, 1<<20)
	require.NoError(t, err)
	require.Len(t, page.Facts, 1)
	assert.Equal(t, fact.AttemptID, page.Facts[0].AttemptID)
	assert.Equal(t, uint64(1), page.ObservedTotals.Facts)
	assert.Equal(t, 1, bucket.getCalls)
}

func TestTrajectoryReaderAcceptsMaximumLimit(t *testing.T) {
	const loopID = "loop-max-limit"
	bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
	for i := 1; i <= maxTrajectoryQueryLimit+1; i++ {
		putTrajectoryReaderFact(t, bucket, loopID, trajectoryReaderFact(
			loopID, trajectoryAttemptID(i), uint64(i),
			agentic.TrajectoryKindLoopStarted, agentic.TrajectoryPhaseLoopStart, uint32(i), 0,
		))
	}
	page, err := newTrajectoryReader(bucket).read(context.Background(), agentic.TrajectoryQueryRequest{
		LoopID: loopID,
		Limit:  maxTrajectoryQueryLimit,
	}, 1<<20)
	require.NoError(t, err)
	assert.Len(t, page.Facts, maxTrajectoryQueryLimit)
	assert.NotEmpty(t, page.NextCursor)
}

func TestTrajectoryReaderCursorUsesCompleteCausalTuple(t *testing.T) {
	const loopID = "loop-complete-tuple"
	bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
	facts := []agentic.TrajectoryFactV1{
		trajectoryReaderFact(loopID, "attempt4", 4, agentic.TrajectoryKindToolCompleted, agentic.TrajectoryPhaseToolResult, 1, 0),
		trajectoryReaderFact(loopID, "attempt2", 2, agentic.TrajectoryKindModelCompleted, agentic.TrajectoryPhaseModelResult, 1, 0),
		trajectoryReaderFact(loopID, "attempt3", 3, agentic.TrajectoryKindToolCompleted, agentic.TrajectoryPhaseToolResult, 1, 0),
		trajectoryReaderFact(loopID, "attempt1", 1, agentic.TrajectoryKindModelCompleted, agentic.TrajectoryPhaseModelResult, 1, 0),
	}
	facts[0].CausalOrdinal = 2
	facts[2].CausalOrdinal = 1
	for _, fact := range facts {
		putTrajectoryReaderFact(t, bucket, loopID, fact)
	}

	cursor := ""
	var got []string
	for {
		page, err := newTrajectoryReader(bucket).read(context.Background(), agentic.TrajectoryQueryRequest{
			LoopID: loopID,
			Limit:  1,
			Cursor: cursor,
		}, 1<<20)
		require.NoError(t, err)
		require.Len(t, page.Facts, 1)
		got = append(got, page.Facts[0].AttemptID)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	assert.Equal(t, []string{"attempt1", "attempt2", "attempt3", "attempt4"}, got)
}

func TestTrajectoryCursorPhaseRanksMatchFactSortOrder(t *testing.T) {
	phases := []agentic.TrajectoryPhase{
		agentic.TrajectoryPhaseTerminal,
		agentic.TrajectoryPhaseCompaction,
		agentic.TrajectoryPhaseToolResult,
		agentic.TrajectoryPhaseToolRequest,
		agentic.TrajectoryPhaseModelResult,
		agentic.TrajectoryPhaseModelRequest,
		agentic.TrajectoryPhaseLoopStart,
	}
	facts := make([]agentic.TrajectoryFactV1, len(phases))
	for i, phase := range phases {
		facts[i] = agentic.TrajectoryFactV1{
			AttemptID:       trajectoryAttemptID(i + 1),
			AttemptOrdinal:  uint64(i + 1),
			CausalPhase:     phase,
			EvidenceCapture: agentic.TrajectoryEvidenceNone,
		}
	}
	agentic.SortTrajectoryFacts(facts)
	for wantRank, fact := range facts {
		gotRank, ok := trajectoryCursorPhaseRank(fact.CausalPhase)
		require.True(t, ok)
		assert.Equal(t, wantRank, gotRank)
	}
}

func TestTrajectoryReaderRejectsInvalidCursorBeforeKVList(t *testing.T) {
	const loopID = "loop-cursor"
	validFact := trajectoryReaderFact(loopID, "attempt1", 1,
		agentic.TrajectoryKindLoopStarted, agentic.TrajectoryPhaseLoopStart, 0, 0)
	valid, err := encodeTrajectoryCursor(loopID, validFact)
	require.NoError(t, err)
	foreign, err := encodeTrajectoryCursor("other-loop", validFact)
	require.NoError(t, err)
	canonical := string(mustDecodeCursor(t, valid))

	cursorJSON := func(value string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(value))
	}
	tests := map[string]string{
		"malformed base64":        "!!!",
		"padded base64":           valid + "=",
		"base64 newline":          valid + "\n",
		"unknown field":           cursorJSON(`{"version":"v1","loop_digest":"` + agentic.TrajectoryLoopDigest(loopID) + `","iteration":0,"phase_rank":0,"source_ordinal":0,"attempt_ordinal":1,"attempt_id":"attempt1","extra":true}`),
		"missing iteration":       cursorJSON(strings.Replace(canonical, `"iteration":0,`, "", 1)),
		"missing phase rank":      cursorJSON(strings.Replace(canonical, `"phase_rank":0,`, "", 1)),
		"missing source ordinal":  cursorJSON(strings.Replace(canonical, `"source_ordinal":0,`, "", 1)),
		"missing version":         cursorJSON(strings.Replace(canonical, `"version":"v1",`, "", 1)),
		"missing loop digest":     cursorJSON(strings.Replace(canonical, `"loop_digest":"`+agentic.TrajectoryLoopDigest(loopID)+`",`, "", 1)),
		"missing attempt id":      cursorJSON(strings.Replace(canonical, `,"attempt_id":"attempt1"`, "", 1)),
		"unsupported version":     cursorJSON(strings.Replace(canonical, `"v1"`, `"v2"`, 1)),
		"invalid phase rank":      cursorJSON(strings.Replace(canonical, `"phase_rank":0`, `"phase_rank":-1`, 1)),
		"negative iteration":      cursorJSON(strings.Replace(canonical, `"iteration":0`, `"iteration":-1`, 1)),
		"negative source ordinal": cursorJSON(strings.Replace(canonical, `"source_ordinal":0`, `"source_ordinal":-1`, 1)),
		"invalid attempt ordinal": cursorJSON(strings.Replace(canonical, `"attempt_ordinal":1`, `"attempt_ordinal":0`, 1)),
		"invalid attempt id":      cursorJSON(strings.Replace(canonical, `"attempt_id":"attempt1"`, `"attempt_id":"bad-id"`, 1)),
		"noncanonical whitespace": cursorJSON(" " + canonical),
		"cross loop":              foreign,
	}

	for name, cursor := range tests {
		t.Run(name, func(t *testing.T) {
			bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
			_, readErr := newTrajectoryReader(bucket).read(context.Background(), agentic.TrajectoryQueryRequest{
				LoopID: loopID,
				Cursor: cursor,
			}, 1<<20)
			requireClassifiedTrajectoryError(t, readErr, "invalid_cursor")
			assert.Equal(t, 0, bucket.listCalls)
		})
	}
}

func TestTrajectoryCursorEncodingIsUnpaddedCanonicalV1(t *testing.T) {
	const loopID = "loop-canonical-cursor"
	fact := trajectoryReaderFact(loopID, "attempt9", 9,
		agentic.TrajectoryKindToolCompleted, agentic.TrajectoryPhaseToolResult, 7, 0)
	fact.CausalOrdinal = 3

	token, err := encodeTrajectoryCursor(loopID, fact)
	require.NoError(t, err)
	assert.NotContains(t, token, "=")
	decoded := mustDecodeCursor(t, token)
	want := fmt.Sprintf(
		`{"version":"v1","loop_digest":"%s","iteration":7,"phase_rank":4,"source_ordinal":3,"attempt_ordinal":9,"attempt_id":"attempt9"}`,
		agentic.TrajectoryLoopDigest(loopID),
	)
	assert.Equal(t, want, string(decoded))
}

func TestTrajectoryQueryRejectsUnknownHydrationAndInvalidLimitsBeforeKVList(t *testing.T) {
	tests := map[string]string{
		"hydration":      `{"loopId":"loop-1","hydrateEvidence":true}`,
		"unknown field":  `{"loopId":"loop-1","unexpected":true}`,
		"negative limit": `{"loopId":"loop-1","limit":-1}`,
		"above maximum":  `{"loopId":"loop-1","limit":257}`,
	}
	for name, request := range tests {
		t.Run(name, func(t *testing.T) {
			bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
			component := &Component{trajectoryReader: newTrajectoryReader(bucket)}
			_, err := component.handleTrajectoryQueryWithMaxPayload(context.Background(), []byte(request), 1<<20)
			requireClassifiedTrajectoryError(t, err, "invalid_request")
			assert.Equal(t, 0, bucket.listCalls)
		})
	}
}

func TestTrajectoryReaderExactPageFitAndIndivisibleRefusal(t *testing.T) {
	const loopID = "loop-fit"
	bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
	firstFact := trajectoryReaderFact(loopID, "attempt1", 1,
		agentic.TrajectoryKindModelCompleted, agentic.TrajectoryPhaseModelResult, 1, 1)
	secondFact := trajectoryReaderFact(loopID, "attempt2", 2,
		agentic.TrajectoryKindModelCompleted, agentic.TrajectoryPhaseModelResult, 2, 1)
	putTrajectoryReaderFact(t, bucket, loopID, firstFact)
	putTrajectoryReaderFact(t, bucket, loopID, secondFact)

	cursor, err := encodeTrajectoryCursor(loopID, firstFact)
	require.NoError(t, err)
	want := agentic.TrajectoryPage{
		SchemaVersion: agentic.TrajectorySchemaV1,
		LoopID:        loopID,
		Coverage:      trajectoryCoverageObserved,
		ObservedTotals: agentic.TrajectoryObservedTotals{
			Facts:            1,
			ElapsedMS:        1,
			ModelCompletions: 1,
		},
		Facts:      []agentic.TrajectoryFactV1{firstFact},
		NextCursor: cursor,
	}
	wantBytes, err := json.Marshal(want)
	require.NoError(t, err)

	reader := newTrajectoryReader(bucket)
	page, err := reader.read(context.Background(), agentic.TrajectoryQueryRequest{LoopID: loopID}, int64(len(wantBytes)))
	require.NoError(t, err)
	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	assert.Equal(t, wantBytes, encoded)

	_, err = reader.read(context.Background(), agentic.TrajectoryQueryRequest{LoopID: loopID}, int64(len(wantBytes)-1))
	requireClassifiedTrajectoryError(t, err, "response_too_large")
	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	assert.Equal(t, int64(len(wantBytes)), classified.Detail["response_bytes"])
	assert.Equal(t, int64(len(wantBytes)-1), classified.Detail["max_payload"])
}

func trajectoryAttemptID(value int) string {
	return fmt.Sprintf("attempt%03d", value)
}

func mustDecodeCursor(t *testing.T, cursor string) []byte {
	t.Helper()
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	require.NoError(t, err)
	return decoded
}

func requireClassifiedTrajectoryError(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	assert.Equal(t, errs.ErrorInvalid, classified.Class)
	assert.Equal(t, code, classified.Code)
}
