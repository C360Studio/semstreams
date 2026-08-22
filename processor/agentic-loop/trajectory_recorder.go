package agenticloop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/storage/storeregistry"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

type trajectoryAuditStage string

const (
	trajectoryStageProviderResolve trajectoryAuditStage = "provider_resolve"
	trajectoryStageEvidenceGet     trajectoryAuditStage = "evidence_get"
	trajectoryStageEvidencePut     trajectoryAuditStage = "evidence_put"
	trajectoryStageEvidenceVerify  trajectoryAuditStage = "evidence_verify"
	trajectoryStageFactEncode      trajectoryAuditStage = "fact_encode"
	trajectoryStageFactCreate      trajectoryAuditStage = "fact_create"
	trajectoryStageFactVerify      trajectoryAuditStage = "fact_verify"
)

type trajectoryAuditReason string

const (
	trajectoryReasonProviderUnavailable trajectoryAuditReason = "provider_unavailable"
	trajectoryReasonBackend             trajectoryAuditReason = "backend_error"
	trajectoryReasonIntegrity           trajectoryAuditReason = "integrity_conflict"
	trajectoryReasonEncode              trajectoryAuditReason = "encode_error"
	trajectoryReasonTimeout             trajectoryAuditReason = "timeout"
)

type trajectoryAuditFailure struct {
	Stage     trajectoryAuditStage
	Kind      agentic.TrajectoryKind
	Reason    trajectoryAuditReason
	LoopID    string
	AttemptID string
	Err       error

	// Late marks a failure discovered after the context owning its
	// recording attempt was already done — an abandoned batch goroutine
	// unwinding past its framework budget.
	//
	// The failure is no less REAL for being late: a store.Put that returned
	// a backend error at T+240ms is a genuine evidence_put/backend_error and
	// the operator needs its stage and reason, not just the synthetic
	// fact_create/timeout the budget branch reports. So a late failure still
	// logs, still increments the bounded counter, and still latches Health.
	//
	// What it must NOT do is MARK the loop. The loop reached its terminal
	// write and released its per-loop state before this report existed;
	// marking now re-inserts a marker nothing will free, and a later loop
	// reusing the same loop ID inherits a condition that is not its own.
	// The mark is the one sink for which "late" makes the answer wrong
	// rather than merely tardy, and it is the only sink Late suppresses.
	Late bool
}

type trajectoryFactBucket interface {
	Create(context.Context, string, []byte, ...jetstream.KVCreateOpt) (uint64, error)
	Get(context.Context, string) (jetstream.KeyValueEntry, error)
	ListKeysFiltered(context.Context, ...string) (jetstream.KeyLister, error)
}

type trajectoryObservation struct {
	LoopID            string
	Kind              agentic.TrajectoryKind
	SourceKind        agentic.TrajectorySourceKind
	SourceCorrelation string
	CausalIteration   uint32
	CausalPhase       agentic.TrajectoryPhase
	CausalOrdinal     uint32
	ObservedAt        time.Time
	ElapsedMS         int64
	Status            agentic.TrajectoryStatus
	TokensIn          uint64
	TokensOut         uint64
	MessageCount      uint32
	ToolCount         uint32
	URLCount          uint32
	ModelPreview      string
	ProviderPreview   string
	ToolPreview       string
	CapabilityPreview string
	ErrorCategory     agentic.TrajectoryErrorCategory
	Evidence          any
}

type trajectoryRecordResult struct {
	Key        string
	Bytes      []byte
	Fact       agentic.TrajectoryFactV1
	FactStored bool
}

type trajectoryAttempt struct {
	ID         string
	Ordinal    uint64
	ObservedAt time.Time
}

type trajectoryLoopOrdinal struct {
	mu          sync.Mutex
	initialized bool
	next        uint64
	batchToken  chan struct{}
}

// trajectoryRecorder owns immutable fact creation. It holds no storage handle:
// evidence resolution goes through StoreRegistry on every operation.
type trajectoryRecorder struct {
	bucket          trajectoryFactBucket
	stores          *storeregistry.Registry
	storageInstance string
	report          func(trajectoryAuditFailure)

	ordinalMu     sync.Mutex
	ordinalByLoop map[string]*trajectoryLoopOrdinal
	newAttemptID  func() string
	now           func() time.Time
}

func newTrajectoryRecorder(
	bucket trajectoryFactBucket,
	stores *storeregistry.Registry,
	storageInstance string,
	report func(trajectoryAuditFailure),
) *trajectoryRecorder {
	return &trajectoryRecorder{
		bucket: bucket, stores: stores, storageInstance: storageInstance, report: report,
		ordinalByLoop: make(map[string]*trajectoryLoopOrdinal),
		newAttemptID:  func() string { return strings.ReplaceAll(uuid.NewString(), "-", "") },
		now:           time.Now,
	}
}

func (r *trajectoryRecorder) record(ctx context.Context, observation trajectoryObservation) trajectoryRecordResult {
	attempt, err := r.allocateAttempt(ctx, observation.LoopID, observation.Kind)
	if err != nil {
		return trajectoryRecordResult{Fact: agentic.TrajectoryFactV1{AttemptID: attempt.ID}}
	}
	if ctx.Err() != nil {
		return trajectoryRecordResult{Fact: agentic.TrajectoryFactV1{AttemptID: attempt.ID, AttemptOrdinal: attempt.Ordinal}}
	}
	if observation.ObservedAt.IsZero() {
		observation.ObservedAt = attempt.ObservedAt
	}

	fact := agentic.TrajectoryFactV1{
		SchemaVersion:     agentic.TrajectorySchemaV1,
		LoopDigest:        agentic.TrajectoryLoopDigest(observation.LoopID),
		AttemptID:         attempt.ID,
		AttemptOrdinal:    attempt.Ordinal,
		Kind:              observation.Kind,
		SourceKind:        observation.SourceKind,
		SourceCorrelation: observation.SourceCorrelation,
		CausalIteration:   observation.CausalIteration,
		CausalPhase:       observation.CausalPhase,
		CausalOrdinal:     observation.CausalOrdinal,
		ObservedAt:        observation.ObservedAt,
		ElapsedMS:         observation.ElapsedMS,
		Status:            observation.Status,
		TokensIn:          observation.TokensIn,
		TokensOut:         observation.TokensOut,
		MessageCount:      observation.MessageCount,
		ToolCount:         observation.ToolCount,
		URLCount:          observation.URLCount,
		ModelPreview:      observation.ModelPreview,
		ProviderPreview:   observation.ProviderPreview,
		ToolPreview:       observation.ToolPreview,
		CapabilityPreview: observation.CapabilityPreview,
		ErrorCategory:     observation.ErrorCategory,
		EvidenceCapture:   agentic.TrajectoryEvidenceNone,
	}

	if observation.Evidence != nil {
		capture := r.captureEvidence(ctx, observation.Kind, observation.Evidence, observation.LoopID, attempt.ID)
		fact.EvidenceDigest = capture.digest
		fact.EvidenceSize = uint64(capture.size)
		fact.EvidenceCapture = capture.state
		fact.EvidenceFailure = capture.failure
		fact.Evidence = capture.reference
		if ctx.Err() != nil {
			return trajectoryRecordResult{Fact: fact}
		}
	}

	key, err := agentic.TrajectoryFactKey(observation.LoopID, attempt.ID)
	if err != nil {
		r.fail(ctx, observation, attempt.ID, trajectoryStageFactEncode, trajectoryReasonEncode, err)
		return trajectoryRecordResult{Fact: fact}
	}
	encoded, err := fact.CanonicalBytes()
	if err != nil {
		r.fail(ctx, observation, attempt.ID, trajectoryStageFactEncode, trajectoryReasonEncode, err)
		return trajectoryRecordResult{Key: key, Fact: fact}
	}
	result := trajectoryRecordResult{Key: key, Bytes: encoded, Fact: fact}
	if r.bucket == nil {
		r.fail(ctx, observation, attempt.ID, trajectoryStageFactCreate, trajectoryReasonProviderUnavailable,
			fmt.Errorf("trajectory fact bucket unavailable"))
		return result
	}

	if ctx.Err() != nil {
		return result
	}
	_, createErr := r.bucket.Create(ctx, key, encoded)
	if createErr == nil {
		result.FactStored = true
		return result
	}
	if ctx.Err() != nil {
		return result
	}
	entry, getErr := r.bucket.Get(ctx, key)
	if getErr == nil && bytes.Equal(entry.Value(), encoded) {
		result.FactStored = true
		return result
	}
	if getErr == nil {
		r.fail(ctx, observation, attempt.ID, trajectoryStageFactVerify, trajectoryReasonIntegrity,
			fmt.Errorf("immutable fact key contains different canonical bytes"))
		return result
	}
	stage := trajectoryStageFactCreate
	if errors.Is(createErr, jetstream.ErrKeyExists) {
		stage = trajectoryStageFactVerify
	}
	r.fail(ctx, observation, attempt.ID, stage, trajectoryReasonBackend,
		fmt.Errorf("create failed: %v; verification failed: %w", createErr, getErr))
	return result
}

func (r *trajectoryRecorder) allocateAttempt(ctx context.Context, loopID string, kind agentic.TrajectoryKind) (trajectoryAttempt, error) {
	attempt := trajectoryAttempt{ID: r.newAttemptID(), ObservedAt: r.now().UTC()}
	state := r.loopOrdinal(loopID)
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.initialized {
		maxOrdinal, err := maximumVisibleAttemptOrdinal(ctx, r.bucket, loopID)
		if err != nil {
			r.emit(ctx, trajectoryAuditFailure{
				Stage: trajectoryStageFactVerify, Kind: kind, Reason: trajectoryReasonBackend,
				LoopID: loopID, AttemptID: attempt.ID, Err: fmt.Errorf("initialize attempt ordinal: %w", err),
			})
			return attempt, err
		}
		state.next = maxOrdinal
		state.initialized = true
	}
	state.next++
	attempt.Ordinal = state.next
	return attempt, nil
}

func (r *trajectoryRecorder) loopOrdinal(loopID string) *trajectoryLoopOrdinal {
	r.ordinalMu.Lock()
	defer r.ordinalMu.Unlock()
	state := r.ordinalByLoop[loopID]
	if state == nil {
		state = &trajectoryLoopOrdinal{batchToken: make(chan struct{}, 1)}
		state.batchToken <- struct{}{}
		r.ordinalByLoop[loopID] = state
	}
	return state
}

func (r *trajectoryRecorder) acquireLoopBatch(ctx context.Context, loopID string) (func(), bool) {
	state := r.loopOrdinal(loopID)
	select {
	case <-ctx.Done():
		return nil, false
	case <-state.batchToken:
		return func() { state.batchToken <- struct{}{} }, true
	}
}

func (r *trajectoryRecorder) fail(ctx context.Context, observation trajectoryObservation, attemptID string, stage trajectoryAuditStage, reason trajectoryAuditReason, err error) {
	r.emit(ctx, trajectoryAuditFailure{
		Stage: stage, Kind: observation.Kind, Reason: reason,
		LoopID: observation.LoopID, AttemptID: attemptID, Err: err,
	})
}

// emit reports one observed audit failure to the component's fan-out,
// flagging it Late when the context that owns this recording attempt is
// already done.
//
// This is the single classification point for lateness, so every emit site
// in the recorder inherits it — the class, not the instances that motivated
// it. The only production caller of record is recordTrajectoryBatchWithin,
// which abandons its goroutine ONLY after ctx.Done fires, so an abandoned
// attempt's ctx is done for its whole remaining life and every report it
// makes is flagged.
//
// Note precisely what the budget branch already reported and what it did
// not. It reported the LOSS, synchronously, in time for the terminal graph
// write — which is why a late report must not mark the loop again. It did
// NOT report this failure's CLASSIFICATION: the budget branch can only
// say fact_create/timeout, because at that moment nothing more specific is
// known. A late evidence_put/backend_error is new and true information, and
// dropping it would let a payload-size rejection be diagnosed as a latency
// problem. So lateness suppresses the mark alone; the ERROR line, the
// bounded {stage,kind,reason} counter, and the Health latch all still fire
// (see reportTrajectoryAuditFailure).
//
// A healthy attempt is unaffected: ctx.Err() is nil for its whole life, so
// Late stays false and the fan-out behaves exactly as before.
func (r *trajectoryRecorder) emit(ctx context.Context, failure trajectoryAuditFailure) {
	if ctx.Err() != nil {
		failure.Late = true
	}
	if r.report != nil {
		r.report(failure)
	}
}

func evidenceReference(instance, key string, size int) *message.StorageReference {
	return &message.StorageReference{
		StorageInstance: instance,
		Key:             key,
		ContentType:     agentic.TrajectoryEvidenceContentType,
		Size:            int64(size),
	}
}
