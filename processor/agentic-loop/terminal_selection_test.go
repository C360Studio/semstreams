package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type terminalSelectionBucket struct{ *approvalRevisionBucket }

func (b *terminalSelectionBucket) Create(ctx context.Context, key string, value []byte, _ ...jetstream.KVCreateOpt) (uint64, error) {
	if _, exists := b.values[key]; exists {
		return 0, jetstream.ErrKeyExists
	}
	return b.Put(ctx, key, value)
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestTerminalSelectionPreservesSavedOutcome(t *testing.T) {
	for _, outcome := range []string{agentic.OutcomeSuccess, agentic.OutcomeFailed, agentic.OutcomeCancelled} {
		t.Run(outcome, func(t *testing.T) {
			f := newApprovalRecoveryFixture(t)
			require.NoError(t, f.entity.ResolveApproval())
			f.bucket.values[f.entity.ID] = settlementLoopRecord(t, f.entity)
			f.c.loopsBucket = &terminalSelectionBucket{f.c.loopsBucket.(*approvalRevisionBucket)}
			_, revision, revisionErr := f.c.readLoopEntityRevision(t.Context(), f.entity.ID)
			require.NoError(t, revisionErr)
			require.NoError(t, f.c.handler.loopManager.restoreLoopFromRequest(f.entity, f.request, nil))
			var saved message.Payload
			switch outcome {
			case agentic.OutcomeSuccess:
				saved = &agentic.LoopCompletedEvent{LoopID: f.entity.ID, TaskID: f.entity.TaskID,
					Outcome: outcome, Result: "selected result", CompletedAt: time.Now().Add(-time.Minute)}
			case agentic.OutcomeFailed:
				saved = &agentic.LoopFailedEvent{LoopID: f.entity.ID, TaskID: f.entity.TaskID,
					Outcome: outcome, Reason: "selected failure", Error: "selected error", FailedAt: time.Now().Add(-time.Minute)}
			case agentic.OutcomeCancelled:
				saved = &agentic.LoopCancelledEvent{LoopID: f.entity.ID, TaskID: f.entity.TaskID,
					Outcome: outcome, CancelledBy: "original user", CancelledAt: time.Now().Add(-time.Minute)}
			}
			data, err := json.Marshal(saved)
			require.NoError(t, err)
			f.bucket.values["COMPLETE_"+f.entity.ID] = data
			result := HandlerResult{LoopID: f.entity.ID}
			require.NoError(t, f.c.handler.handleCompleteResponse(&result, f.entity.ID, f.entity, "losing candidate", nil))

			// This losing input has no applied proof; it must Retry without changing
			// selected content or the current nonterminal authority. No native PubAck is claimed.
			before := append([]byte(nil), f.bucket.values[f.entity.ID]...)
			err = f.c.persistHandlerResult(t.Context(), result, revision)
			require.Error(t, err)
			assert.False(t, errs.IsFatal(err))
			assert.Equal(t, before, f.bucket.values[f.entity.ID])
			assert.Equal(t, revision, f.c.loopsBucket.(*terminalSelectionBucket).revisions[f.entity.ID])
			assert.Equal(t, string(data), string(f.bucket.values["COMPLETE_"+f.entity.ID]))
			var final agentic.LoopEntity
			require.NoError(t, json.Unmarshal(f.bucket.values[f.entity.ID], &final))
			assert.NotEqual(t, "losing candidate", final.Result)
			if final.State.IsTerminal() {
				assert.Equal(t, outcome, final.Outcome)
			}
		})
	}
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestCompletionCodecPreservesSyntheticActionObligation(t *testing.T) {
	f := newApprovalRecoveryFixture(t)
	var completion agentic.LoopCompletedEvent
	require.NoError(t, json.Unmarshal([]byte(`{"loop_id":"`+f.entity.ID+`","task_id":"`+f.entity.TaskID+
		`","outcome":"success","result":"saved reason","synthetic_decide_required":true}`), &completion))
	decoded, err := f.c.decoder.Decode(settlementEnvelope(t, &completion))
	require.NoError(t, err)
	roundTrip, err := json.Marshal(decoded.Payload())
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(roundTrip, &fields))
	assert.Equal(t, true, fields["synthetic_decide_required"])
	assert.NotContains(t, fields, "decision")
}

// spec: agentic-loop / LoopEntity has one operational state contract
func TestTerminalSelectionRefusesContradictoryPreparedState(t *testing.T) {
	f := newApprovalRecoveryFixture(t)
	pending := f.entity.PendingApproval
	require.NoError(t, f.entity.ResolveApproval())
	f.bucket.values[f.entity.ID] = settlementLoopRecord(t, f.entity)
	f.c.loopsBucket = &terminalSelectionBucket{f.c.loopsBucket.(*approvalRevisionBucket)}
	_, revision, revisionErr := f.c.readLoopEntityRevision(t.Context(), f.entity.ID)
	require.NoError(t, revisionErr)
	require.NoError(t, f.c.handler.loopManager.restoreLoopFromRequest(f.entity, f.request, nil))
	before := append([]byte(nil), f.bucket.values[f.entity.ID]...)
	result := HandlerResult{LoopID: f.entity.ID}
	require.NoError(t, f.c.handler.handleCompleteResponse(&result, f.entity.ID, f.entity, "done", nil))
	// Inject a contradictory prepared record, not a valid gate transition.
	f.c.handler.loopManager.loops[f.entity.ID].PendingApproval = pending
	err := f.c.persistHandlerResult(t.Context(), result, revision)
	require.Error(t, err)
	assert.True(t, errs.IsFatal(err))
	assert.NotContains(t, f.bucket.values, "COMPLETE_"+f.entity.ID)
	assert.Equal(t, before, f.bucket.values[f.entity.ID])
}

// spec: agentic-loop / LoopEntity has one operational state contract
func TestTerminalSelectionPreservesTruncatedMarker(t *testing.T) {
	f := newApprovalRecoveryFixture(t)
	require.NoError(t, f.entity.ResolveApproval())
	f.bucket.values[f.entity.ID] = settlementLoopRecord(t, f.entity)
	f.c.loopsBucket = &terminalSelectionBucket{f.c.loopsBucket.(*approvalRevisionBucket)}
	_, revision, revisionErr := f.c.readLoopEntityRevision(t.Context(), f.entity.ID)
	require.NoError(t, revisionErr)
	require.NoError(t, f.c.handler.loopManager.restoreLoopFromRequest(f.entity, f.request, nil))
	result := HandlerResult{LoopID: f.entity.ID}
	require.NoError(t, f.c.handler.failLoop(&result, f.entity.ID, agentic.OutcomeTruncated, "length_truncated", "output exhausted"))
	require.NoError(t, f.c.persistHandlerResult(t.Context(), result, revision))
	var marker agentic.LoopEntity
	require.NoError(t, json.Unmarshal(f.bucket.values[f.entity.ID], &marker))
	assert.Equal(t, agentic.OutcomeTruncated, marker.Outcome)
	var saved agentic.LoopFailedEvent
	require.NoError(t, json.Unmarshal(f.bucket.values["COMPLETE_"+f.entity.ID], &saved))
	assert.Equal(t, agentic.OutcomeFailed, saved.Outcome)
	assert.Equal(t, "length_truncated", saved.Reason)
}

// spec: agentic-loop / LoopEntity has one operational state contract
func TestTerminalSelectionRejectsChangedSupportingRevision(t *testing.T) {
	f := newApprovalRecoveryFixture(t)
	require.NoError(t, f.entity.ResolveApproval())
	f.bucket.values[f.entity.ID] = settlementLoopRecord(t, f.entity)
	bucket := &terminalSelectionBucket{f.c.loopsBucket.(*approvalRevisionBucket)}
	f.c.loopsBucket = bucket
	_, revision, err := f.c.readLoopEntityRevision(t.Context(), f.entity.ID)
	require.NoError(t, err)
	require.Equal(t, uint64(1), revision)
	require.NoError(t, f.c.handler.loopManager.restoreLoopFromRequest(f.entity, f.request, nil))
	result := HandlerResult{LoopID: f.entity.ID}
	require.NoError(t, f.c.handler.handleCompleteResponse(&result, f.entity.ID, f.entity, "old result", nil))
	newer := f.entity
	require.NoError(t, newer.BeginAwaitingApproval("new-call", "new-tool", nil, "new review", time.Hour, ""))
	newerBytes := settlementLoopRecord(t, newer)
	_, err = bucket.Update(t.Context(), f.entity.ID, newerBytes, revision)
	require.NoError(t, err)
	err = f.c.persistHandlerResult(t.Context(), result, revision)
	require.Error(t, err)
	assert.NotContains(t, f.bucket.values, "COMPLETE_"+f.entity.ID)
	assert.Equal(t, newerBytes, f.bucket.values[f.entity.ID])
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestTerminalSelectionRefusesCandidateMarkerMismatchBeforeSelection(t *testing.T) {
	f := newApprovalRecoveryFixture(t)
	require.NoError(t, f.entity.ResolveApproval())
	f.bucket.values[f.entity.ID] = settlementLoopRecord(t, f.entity)
	f.c.loopsBucket = &terminalSelectionBucket{f.c.loopsBucket.(*approvalRevisionBucket)}
	_, revision, err := f.c.readLoopEntityRevision(t.Context(), f.entity.ID)
	require.NoError(t, err)
	require.NoError(t, f.c.handler.loopManager.restoreLoopFromRequest(f.entity, f.request, nil))
	before := append([]byte(nil), f.bucket.values[f.entity.ID]...)
	result := HandlerResult{LoopID: f.entity.ID}
	require.NoError(t, f.c.handler.handleCompleteResponse(&result, f.entity.ID, f.entity, "prepared result", nil))
	prepared, err := f.c.handler.GetLoop(f.entity.ID)
	require.NoError(t, err)
	require.NoError(t, prepared.Validate(), "local coherence alone must not authorize an unrelated candidate")
	result.CompletionState.Result = "unrelated candidate"

	err = f.c.persistHandlerResult(t.Context(), result, revision)
	require.Error(t, err)
	assert.True(t, errs.IsFatal(err))
	assert.NotContains(t, f.bucket.values, "COMPLETE_"+f.entity.ID)
	assert.Equal(t, before, f.bucket.values[f.entity.ID])
	assert.Equal(t, revision, f.c.loopsBucket.(*terminalSelectionBucket).revisions[f.entity.ID])
	_, lookupErr := f.c.handler.GetLoop(f.entity.ID)
	assert.Error(t, lookupErr, "refused terminal attempt must release speculative process state")
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestTerminalSelectionReplaysStoredSuccess(t *testing.T) {
	f := newApprovalRecoveryFixture(t)
	require.NoError(t, f.entity.ResolveApproval())
	f.bucket.values[f.entity.ID] = settlementLoopRecord(t, f.entity)
	bucket := &terminalSelectionBucket{f.c.loopsBucket.(*approvalRevisionBucket)}
	saved := &agentic.LoopCompletedEvent{
		LoopID: f.entity.ID, TaskID: f.entity.TaskID, Outcome: agentic.OutcomeSuccess,
		Result: "selected result", CompletedAt: time.Date(2026, time.September, 12, 10, 0, 0, 0, time.UTC),
		SyntheticDecideRequired: true,
	}
	savedBytes, err := json.Marshal(saved)
	require.NoError(t, err)
	f.bucket.values["COMPLETE_"+f.entity.ID] = savedBytes
	// Replacement uses only current KV plus this exact retained request. It does
	// not rerun a model/tool. The nil graph/client unit seam is not PubAck proof.
	c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
	c.loopsBucket, c.settlementEvidence = bucket, f.evidence
	response := &agentic.AgentResponse{RequestID: f.request.RequestID, Status: agentic.StatusComplete,
		Message: agentic.ChatMessage{Role: "assistant", Content: saved.Result}}
	decision, err := c.handleResponseMessage(t.Context(), settlementEnvelope(t, response))
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	require.Equal(t, savedBytes, f.bucket.values["COMPLETE_"+f.entity.ID])
	var marker agentic.LoopEntity
	require.NoError(t, json.Unmarshal(f.bucket.values[f.entity.ID], &marker))
	require.NoError(t, marker.Validate())
	assert.Equal(t, agentic.LoopStateComplete, marker.State)
	assert.Equal(t, saved.Result, marker.Result)
	assert.True(t, marker.CompletedAt.Equal(saved.CompletedAt), "final marker must use the selected timestamp")
	assert.Equal(t, f.entity.PendingToolResults, marker.PendingToolResults, "selected content does not replace exact source evidence")
	var retained agentic.LoopCompletedEvent
	require.NoError(t, json.Unmarshal(f.bucket.values["COMPLETE_"+f.entity.ID], &retained))
	assert.True(t, retained.SyntheticDecideRequired)
	_, err = c.handler.GetLoop(f.entity.ID)
	assert.Error(t, err, "settled replacement retained speculative process state")
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestSelectedSyntheticActionFailureWithholdsTerminalMarker(t *testing.T) {
	f := newApprovalRecoveryFixture(t)
	require.NoError(t, f.entity.ResolveApproval())
	f.bucket.values[f.entity.ID] = settlementLoopRecord(t, f.entity)
	bucket := &terminalSelectionBucket{f.c.loopsBucket.(*approvalRevisionBucket)}
	f.c.loopsBucket = bucket
	_, revision, err := f.c.readLoopEntityRevision(t.Context(), f.entity.ID)
	require.NoError(t, err)
	before := append([]byte(nil), f.bucket.values[f.entity.ID]...)
	require.NoError(t, f.c.handler.loopManager.restoreLoopFromRequest(f.entity, f.request, nil))
	result := HandlerResult{LoopID: f.entity.ID}
	require.NoError(t, f.c.handler.handleCompleteResponse(&result, f.entity.ID, f.entity, "selected result", nil))
	result.CompletionState.SyntheticDecideRequired = false
	result.SyntheticDecide = nil
	saved := *result.CompletionState
	saved.SyntheticDecideRequired = true
	saved.CompletedAt = time.Date(2026, time.September, 12, 10, 0, 0, 0, time.UTC)
	savedBytes, err := json.Marshal(&saved)
	require.NoError(t, err)
	f.bucket.values["COMPLETE_"+f.entity.ID] = savedBytes
	f.c.graphWriter = &graphWriter{natsClient: &natsclient.Client{},
		platform: types.PlatformMeta{Org: "acme", Platform: "ops"}, logger: f.c.logger}

	decision, err := f.c.persistTerminalOutcome(t.Context(), result, result.CompletionState, revision)
	require.ErrorContains(t, err, "synthetic decide graph stamp", "saved true obligation must survive the recomputed false candidate")
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision, "required-effect failure must preserve approval's no-settlement class")
	assert.Equal(t, savedBytes, f.bucket.values["COMPLETE_"+f.entity.ID])
	assert.Equal(t, before, f.bucket.values[f.entity.ID])
	assert.Equal(t, revision, bucket.revisions[f.entity.ID])
	_, err = f.c.handler.GetLoop(f.entity.ID)
	assert.Error(t, err)
	// Real graph dependency restoration, absence of native publication on failure,
	// and publication-before-marker on retry are the existing native evidence gate.
}

type terminalSelectionFaultBucket struct {
	*terminalSelectionBucket
	key       string
	createErr error
	getErr    error
}

func (b *terminalSelectionFaultBucket) Create(ctx context.Context, key string, data []byte, opts ...jetstream.KVCreateOpt) (uint64, error) {
	if key == b.key && b.createErr != nil {
		return 0, b.createErr
	}
	return b.terminalSelectionBucket.Create(ctx, key, data, opts...)
}

func (b *terminalSelectionFaultBucket) Get(ctx context.Context, key string) (jetstream.KeyValueEntry, error) {
	if key == b.key && b.getErr != nil {
		return nil, b.getErr
	}
	return b.terminalSelectionBucket.Get(ctx, key)
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestTerminalSelectionRejectsPoisonAndUncertainStorage(t *testing.T) {
	for _, name := range []string{"malformed_json", "wrong_loop", "wrong_task", "invalid_outcome", "invalid_decision", "create_uncertain", "collision_get_uncertain"} {
		t.Run(name, func(t *testing.T) {
			f := newApprovalRecoveryFixture(t)
			require.NoError(t, f.entity.ResolveApproval())
			f.bucket.values[f.entity.ID] = settlementLoopRecord(t, f.entity)
			bucket := &terminalSelectionFaultBucket{
				terminalSelectionBucket: &terminalSelectionBucket{f.c.loopsBucket.(*approvalRevisionBucket)},
				key:                     "COMPLETE_" + f.entity.ID,
			}
			f.c.loopsBucket = bucket
			_, revision, err := f.c.readLoopEntityRevision(t.Context(), f.entity.ID)
			require.NoError(t, err)
			before := append([]byte(nil), f.bucket.values[f.entity.ID]...)
			require.NoError(t, f.c.handler.loopManager.restoreLoopFromRequest(f.entity, f.request, nil))
			result := HandlerResult{LoopID: f.entity.ID}
			require.NoError(t, f.c.handler.handleCompleteResponse(&result, f.entity.ID, f.entity, "selected result", nil))
			saved := *result.CompletionState
			storageErr := errors.New("selected terminal storage unavailable")
			uncertain := false
			switch name {
			case "wrong_loop":
				saved.LoopID = "other-loop"
			case "wrong_task":
				saved.TaskID = "other-task"
			case "invalid_outcome":
				saved.Outcome = "unknown"
			case "invalid_decision":
				saved.Decision = &agentic.CoordinatorDecision{}
			case "create_uncertain":
				bucket.createErr, uncertain = storageErr, true
			case "collision_get_uncertain":
				bucket.getErr, uncertain = storageErr, true
			}
			savedBytes, err := json.Marshal(&saved)
			require.NoError(t, err)
			if name == "malformed_json" {
				savedBytes = []byte("{")
			}
			if name != "create_uncertain" {
				f.bucket.values[bucket.key] = savedBytes
			}

			decision, err := f.c.persistTerminalOutcome(t.Context(), result, result.CompletionState, revision)
			require.Error(t, err)
			if uncertain {
				require.ErrorIs(t, err, storageErr)
				assert.False(t, errs.IsFatal(err))
				assert.Equal(t, natsclient.DeliveryDecisionRetry, decision)
			} else {
				assert.True(t, errs.IsFatal(err))
				assert.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
			}
			if name == "create_uncertain" {
				assert.NotContains(t, f.bucket.values, bucket.key)
			} else {
				assert.Equal(t, savedBytes, f.bucket.values[bucket.key])
			}
			assert.Equal(t, before, f.bucket.values[f.entity.ID])
			assert.Equal(t, revision, bucket.revisions[f.entity.ID])
			_, lookupErr := f.c.handler.GetLoop(f.entity.ID)
			assert.Error(t, lookupErr, "refused selection retained speculative process state")
		})
	}
}
