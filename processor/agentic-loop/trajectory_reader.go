package agenticloop

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

const (
	trajectoryCoverageObserved = "observed"

	defaultTrajectoryQueryLimit = 64
	maxTrajectoryQueryLimit     = 256
	trajectoryCursorVersion     = "v1"
	trajectoryInvalidCursorCode = "invalid_cursor"
)

var errTrajectoryNotFound = errors.New("trajectory not found")

type trajectoryCursorV1 struct {
	Version        string `json:"version"`
	LoopDigest     string `json:"loop_digest"`
	Iteration      uint32 `json:"iteration"`
	PhaseRank      int    `json:"phase_rank"`
	SourceOrdinal  uint32 `json:"source_ordinal"`
	AttemptOrdinal uint64 `json:"attempt_ordinal"`
	AttemptID      string `json:"attempt_id"`
}

type trajectoryReader struct {
	bucket trajectoryFactBucket
}

func newTrajectoryReader(bucket trajectoryFactBucket) *trajectoryReader {
	return &trajectoryReader{bucket: bucket}
}

func decodeTrajectoryQueryRequest(data []byte) (agentic.TrajectoryQueryRequest, error) {
	var request agentic.TrajectoryQueryRequest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest,
			fmt.Errorf("invalid trajectory request: %w", err))
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values are not admitted")
		}
		return request, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest,
			fmt.Errorf("invalid trajectory request: %w", err))
	}
	return request, nil
}

func (r *trajectoryReader) read(
	ctx context.Context,
	request agentic.TrajectoryQueryRequest,
	maxPayload int64,
) (agentic.TrajectoryPage, error) {
	limit, cursor, err := validateTrajectoryReadRequest(request)
	if err != nil {
		return agentic.TrajectoryPage{}, err
	}
	if r.bucket == nil {
		return agentic.TrajectoryPage{}, fmt.Errorf("trajectory fact bucket unavailable")
	}

	prefix := agentic.TrajectoryFactPrefix(request.LoopID)
	keys, err := natsclient.FilteredKeys(ctx, r.bucket, prefix+">")
	if err != nil {
		return agentic.TrajectoryPage{}, fmt.Errorf("list trajectory facts: %w", err)
	}

	facts := make([]agentic.TrajectoryFactV1, 0, len(keys))
	seenKeys := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if _, seen := seenKeys[key]; seen {
			continue
		}
		seenKeys[key] = struct{}{}
		entry, getErr := r.bucket.Get(ctx, key)
		if getErr != nil {
			return agentic.TrajectoryPage{}, fmt.Errorf("get trajectory fact %q: %w", key, getErr)
		}
		fact, decodeErr := decodeTrajectoryReadFact(request.LoopID, key, entry.Value())
		if decodeErr != nil {
			return agentic.TrajectoryPage{}, decodeErr
		}
		facts = append(facts, fact)
	}
	if len(facts) == 0 {
		return agentic.TrajectoryPage{}, errTrajectoryNotFound
	}

	agentic.SortTrajectoryFacts(facts)
	start := 0
	if cursor != nil {
		start = sort.Search(len(facts), func(i int) bool {
			return compareTrajectoryFactToCursor(facts[i], *cursor) > 0
		})
	}
	return fitTrajectoryPage(request.LoopID, facts[start:], limit, maxPayload)
}

func validateTrajectoryReadRequest(request agentic.TrajectoryQueryRequest) (int, *trajectoryCursorV1, error) {
	if request.LoopID == "" {
		return 0, nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest,
			errors.New("loopId required"))
	}
	if request.Limit < 0 {
		return 0, nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest,
			errors.New("limit must be zero or between 1 and 256"))
	}
	if request.Limit > maxTrajectoryQueryLimit {
		return 0, nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest,
			errors.New("limit must not exceed 256"))
	}
	limit := request.Limit
	if limit == 0 {
		limit = defaultTrajectoryQueryLimit
	}
	if request.Cursor == "" {
		return limit, nil, nil
	}
	cursor, err := decodeTrajectoryCursor(request.LoopID, request.Cursor)
	if err != nil {
		return 0, nil, errs.ClassifiedCode(errs.ErrorInvalid, trajectoryInvalidCursorCode,
			fmt.Errorf("invalid trajectory cursor: %w", err))
	}
	return limit, &cursor, nil
}

func fitTrajectoryPage(
	loopID string,
	remaining []agentic.TrajectoryFactV1,
	limit int,
	maxPayload int64,
) (agentic.TrajectoryPage, error) {
	pageCap := len(remaining)
	if pageCap > limit {
		pageCap = limit
	}

	factsArraySizes := make([]int64, pageCap+1)
	factsArraySizes[0] = int64(len(`[]`))
	for i := 0; i < pageCap; i++ {
		encoded, err := json.Marshal(remaining[i])
		if err != nil {
			return agentic.TrajectoryPage{}, fmt.Errorf("marshal trajectory fact %q: %w", remaining[i].AttemptID, err)
		}
		factsArraySizes[i+1] = factsArraySizes[i] + int64(len(encoded))
		if i > 0 {
			factsArraySizes[i+1]++
		}
	}

	firstCount := 1
	if len(remaining) == 0 {
		firstCount = 0
	}
	bestCount := -1
	bestCursor := ""
	firstCandidateBytes := int64(0)
	for count := firstCount; count <= pageCap; count++ {
		nextCursor := ""
		if count < len(remaining) {
			var err error
			nextCursor, err = encodeTrajectoryCursor(loopID, remaining[count-1])
			if err != nil {
				return agentic.TrajectoryPage{}, err
			}
		}
		candidate := trajectoryPage(loopID, remaining[:count], nextCursor)
		candidate.Facts = make([]agentic.TrajectoryFactV1, 0)
		envelopeBytes, err := json.Marshal(candidate)
		if err != nil {
			return agentic.TrajectoryPage{}, fmt.Errorf("marshal trajectory page envelope: %w", err)
		}
		candidateBytes := int64(len(envelopeBytes)-len(`[]`)) + factsArraySizes[count]
		if count == firstCount {
			firstCandidateBytes = candidateBytes
		}
		if candidateBytes <= maxPayload {
			bestCount = count
			bestCursor = nextCursor
		}
	}

	if bestCount < 0 {
		return agentic.TrajectoryPage{}, errs.ClassifiedCodeDetail(
			errs.ErrorInvalid,
			"response_too_large",
			map[string]any{
				"response_bytes": firstCandidateBytes,
				"max_payload":    maxPayload,
			},
			errors.New("trajectory response fact exceeds active NATS maximum payload"),
		)
	}

	page := trajectoryPage(loopID, remaining[:bestCount], bestCursor)
	encoded, err := json.Marshal(page)
	if err != nil {
		return agentic.TrajectoryPage{}, fmt.Errorf("marshal trajectory page: %w", err)
	}
	if int64(len(encoded)) > maxPayload {
		return agentic.TrajectoryPage{}, fmt.Errorf(
			"trajectory page fitting invariant: encoded %d bytes exceeds max payload %d", len(encoded), maxPayload)
	}
	return page, nil
}

func trajectoryPage(loopID string, facts []agentic.TrajectoryFactV1, nextCursor string) agentic.TrajectoryPage {
	page := agentic.TrajectoryPage{
		SchemaVersion: agentic.TrajectorySchemaV1,
		LoopID:        loopID,
		Coverage:      trajectoryCoverageObserved,
		Facts:         facts,
		NextCursor:    nextCursor,
	}
	for i := range facts {
		addTrajectoryObservedTotal(&page.ObservedTotals, facts[i])
	}
	page.TerminalObserved = page.ObservedTotals.TerminalObservations > 0
	return page
}

func encodeTrajectoryCursor(loopID string, fact agentic.TrajectoryFactV1) (string, error) {
	phaseRank, ok := trajectoryCursorPhaseRank(fact.CausalPhase)
	if !ok {
		return "", fmt.Errorf("unknown trajectory phase %q", fact.CausalPhase)
	}
	cursor := trajectoryCursorV1{
		Version:        trajectoryCursorVersion,
		LoopDigest:     agentic.TrajectoryLoopDigest(loopID),
		Iteration:      fact.CausalIteration,
		PhaseRank:      phaseRank,
		SourceOrdinal:  fact.CausalOrdinal,
		AttemptOrdinal: fact.AttemptOrdinal,
		AttemptID:      fact.AttemptID,
	}
	if err := validateTrajectoryCursor(loopID, cursor); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("marshal trajectory cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeTrajectoryCursor(loopID, token string) (trajectoryCursorV1, error) {
	var cursor trajectoryCursorV1
	for _, char := range token {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_') {
			return cursor, fmt.Errorf("cursor contains non-base64url character %q", char)
		}
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return cursor, fmt.Errorf("decode base64url: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return cursor, fmt.Errorf("decode JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return cursor, errors.New("multiple JSON values are not admitted")
		}
		return cursor, fmt.Errorf("decode JSON trailer: %w", err)
	}
	canonical, err := json.Marshal(cursor)
	if err != nil {
		return cursor, fmt.Errorf("marshal canonical cursor: %w", err)
	}
	if !bytes.Equal(canonical, decoded) {
		return cursor, errors.New("cursor JSON is not strict canonical form")
	}
	if err := validateTrajectoryCursor(loopID, cursor); err != nil {
		return cursor, err
	}
	return cursor, nil
}

func validateTrajectoryCursor(loopID string, cursor trajectoryCursorV1) error {
	if cursor.Version != trajectoryCursorVersion {
		return fmt.Errorf("unsupported version %q", cursor.Version)
	}
	if cursor.LoopDigest != agentic.TrajectoryLoopDigest(loopID) {
		return errors.New("cursor loop digest does not match requested loop")
	}
	if cursor.PhaseRank < 0 || cursor.PhaseRank > 6 {
		return fmt.Errorf("phase_rank %d is outside v1 causal order", cursor.PhaseRank)
	}
	if cursor.AttemptOrdinal == 0 || !validTrajectoryCursorAttemptID(cursor.AttemptID) {
		return errors.New("cursor attempt identity is invalid")
	}
	return nil
}

func compareTrajectoryFactToCursor(fact agentic.TrajectoryFactV1, cursor trajectoryCursorV1) int {
	phaseRank, _ := trajectoryCursorPhaseRank(fact.CausalPhase)
	if fact.CausalIteration != cursor.Iteration {
		return compareOrdered(fact.CausalIteration, cursor.Iteration)
	}
	if phaseRank != cursor.PhaseRank {
		return compareOrdered(phaseRank, cursor.PhaseRank)
	}
	if fact.CausalOrdinal != cursor.SourceOrdinal {
		return compareOrdered(fact.CausalOrdinal, cursor.SourceOrdinal)
	}
	if fact.AttemptOrdinal != cursor.AttemptOrdinal {
		return compareOrdered(fact.AttemptOrdinal, cursor.AttemptOrdinal)
	}
	if fact.AttemptID < cursor.AttemptID {
		return -1
	}
	if fact.AttemptID > cursor.AttemptID {
		return 1
	}
	return 0
}

func compareOrdered[T ~int | ~uint32 | ~uint64](left, right T) int {
	if left < right {
		return -1
	}
	return 1
}

func trajectoryCursorPhaseRank(phase agentic.TrajectoryPhase) (int, bool) {
	switch phase {
	case agentic.TrajectoryPhaseLoopStart:
		return 0, true
	case agentic.TrajectoryPhaseModelRequest:
		return 1, true
	case agentic.TrajectoryPhaseModelResult:
		return 2, true
	case agentic.TrajectoryPhaseToolRequest:
		return 3, true
	case agentic.TrajectoryPhaseToolResult:
		return 4, true
	case agentic.TrajectoryPhaseCompaction:
		return 5, true
	case agentic.TrajectoryPhaseTerminal:
		return 6, true
	default:
		return 0, false
	}
}

func validTrajectoryCursorAttemptID(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func decodeTrajectoryReadFact(loopID, key string, encoded []byte) (agentic.TrajectoryFactV1, error) {
	var fact agentic.TrajectoryFactV1
	if err := json.Unmarshal(encoded, &fact); err != nil {
		return fact, fmt.Errorf("decode trajectory fact %q: %w", key, err)
	}
	if fact.LoopDigest != agentic.TrajectoryLoopDigest(loopID) {
		return fact, fmt.Errorf("trajectory fact %q loop digest mismatch", key)
	}
	expectedKey, err := agentic.TrajectoryFactKey(loopID, fact.AttemptID)
	if err != nil {
		return fact, fmt.Errorf("validate trajectory fact %q identity: %w", key, err)
	}
	if key != expectedKey {
		return fact, fmt.Errorf("trajectory fact %q key does not match attempt identity", key)
	}
	canonical, err := fact.CanonicalBytes()
	if err != nil {
		return fact, fmt.Errorf("validate trajectory fact %q: %w", key, err)
	}
	if !bytes.Equal(canonical, encoded) {
		return fact, fmt.Errorf("trajectory fact %q is not canonical", key)
	}
	return fact, nil
}

func addTrajectoryObservedTotal(t *agentic.TrajectoryObservedTotals, fact agentic.TrajectoryFactV1) {
	t.Facts++
	t.TokensIn += fact.TokensIn
	t.TokensOut += fact.TokensOut
	t.ElapsedMS += fact.ElapsedMS
	t.MessageCount += uint64(fact.MessageCount)
	t.ToolCount += uint64(fact.ToolCount)
	t.URLCount += uint64(fact.URLCount)
	switch fact.Kind {
	case agentic.TrajectoryKindModelRequested:
		t.ModelRequests++
	case agentic.TrajectoryKindModelCompleted:
		t.ModelCompletions++
	case agentic.TrajectoryKindToolRequested:
		t.ToolRequests++
	case agentic.TrajectoryKindToolCompleted:
		t.ToolCompletions++
	case agentic.TrajectoryKindContextCompacted:
		t.ContextCompactions++
	case agentic.TrajectoryKindLoopTerminal:
		t.TerminalObservations++
	}
	switch fact.Status {
	case agentic.TrajectoryStatusRequested:
		t.RequestedObservations++
	case agentic.TrajectoryStatusCompleted:
		t.CompletedObservations++
	case agentic.TrajectoryStatusFailed:
		t.FailedObservations++
	case agentic.TrajectoryStatusCancelled:
		t.CancelledObservations++
	}
}
