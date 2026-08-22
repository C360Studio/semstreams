package agenticloop

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/storage"
)

type trajectoryEvidenceCapture struct {
	digest    string
	size      int
	state     agentic.TrajectoryEvidenceCapture
	failure   agentic.TrajectoryEvidenceFailure
	reference *message.StorageReference
}

func (r *trajectoryRecorder) captureEvidence(
	ctx context.Context,
	kind agentic.TrajectoryKind,
	body any,
	loopID, attemptID string,
) trajectoryEvidenceCapture {
	encoded, digest, key, err := agentic.CanonicalTrajectoryEvidence(kind, body)
	if err != nil {
		r.emit(ctx, trajectoryAuditFailure{
			Stage: trajectoryStageFactEncode, Kind: kind, Reason: trajectoryReasonEncode,
			LoopID: loopID, AttemptID: attemptID, Err: err,
		})
		return trajectoryEvidenceCapture{state: agentic.TrajectoryEvidenceMissing, failure: agentic.TrajectoryEvidenceFailureWrite}
	}
	capture := trajectoryEvidenceCapture{
		digest: digest, size: len(encoded), state: agentic.TrajectoryEvidenceMissing,
	}
	if r.stores == nil {
		capture.failure = agentic.TrajectoryEvidenceFailureProviderUnavailable
		r.evidenceFailure(ctx, loopID, attemptID, kind, trajectoryStageProviderResolve, trajectoryReasonProviderUnavailable,
			fmt.Errorf("store registry unavailable"))
		return capture
	}
	store, ok := r.stores.Store(r.storageInstance)
	if !ok {
		capture.failure = agentic.TrajectoryEvidenceFailureProviderUnavailable
		r.evidenceFailure(ctx, loopID, attemptID, kind, trajectoryStageProviderResolve, trajectoryReasonProviderUnavailable,
			fmt.Errorf("storage instance %q unavailable", r.storageInstance))
		return capture
	}

	existing, getErr := store.Get(ctx, key)
	switch {
	case getErr == nil && bytes.Equal(existing, encoded):
		capture.state = agentic.TrajectoryEvidenceStored
		capture.reference = evidenceReference(r.storageInstance, key, len(encoded))
		return capture
	case getErr == nil:
		capture.failure = agentic.TrajectoryEvidenceFailureIntegrity
		r.evidenceFailure(ctx, loopID, attemptID, kind, trajectoryStageEvidenceVerify, trajectoryReasonIntegrity,
			fmt.Errorf("digest-addressed evidence contains different canonical bytes"))
		return capture
	case !errors.Is(getErr, storage.ErrObjectNotFound):
		capture.failure = agentic.TrajectoryEvidenceFailureRead
		r.evidenceFailure(ctx, loopID, attemptID, kind, trajectoryStageEvidenceGet, trajectoryReasonBackend, getErr)
		return capture
	}

	putErr := store.Put(ctx, key, encoded)
	if putErr == nil {
		capture.state = agentic.TrajectoryEvidenceStored
		capture.reference = evidenceReference(r.storageInstance, key, len(encoded))
		return capture
	}

	// Put errors can be lost replies. Resolve lazily again so a provider restart
	// between Put and verification is observed instead of retaining a stale handle.
	verifyStore, ok := r.stores.Store(r.storageInstance)
	if ok {
		verified, verifyErr := verifyStore.Get(ctx, key)
		if verifyErr == nil && bytes.Equal(verified, encoded) {
			capture.state = agentic.TrajectoryEvidenceStored
			capture.reference = evidenceReference(r.storageInstance, key, len(encoded))
			return capture
		}
		if verifyErr == nil {
			capture.failure = agentic.TrajectoryEvidenceFailureIntegrity
			r.evidenceFailure(ctx, loopID, attemptID, kind, trajectoryStageEvidenceVerify, trajectoryReasonIntegrity,
				fmt.Errorf("lost-reply verification found different canonical bytes"))
			return capture
		}
	}
	capture.failure = agentic.TrajectoryEvidenceFailureWrite
	r.evidenceFailure(ctx, loopID, attemptID, kind, trajectoryStageEvidencePut, trajectoryReasonBackend, putErr)
	return capture
}

func (r *trajectoryRecorder) evidenceFailure(
	ctx context.Context,
	loopID, attemptID string,
	kind agentic.TrajectoryKind,
	stage trajectoryAuditStage,
	reason trajectoryAuditReason,
	err error,
) {
	r.emit(ctx, trajectoryAuditFailure{
		Stage: stage, Kind: kind, Reason: reason,
		LoopID: loopID, AttemptID: attemptID, Err: err,
	})
}
