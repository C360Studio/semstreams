package agenticloop

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type approvalClosureEvidence struct {
	loopSettlementEvidenceReader
	afterRequest  func(context.Context)
	afterResponse func(context.Context)
}

func (e *approvalClosureEvidence) ReadAgentRequest(ctx context.Context, stream, subject string) (retainedLoopMessage, bool, error) {
	request, found, err := e.loopSettlementEvidenceReader.ReadAgentRequest(ctx, stream, subject)
	if e.afterRequest != nil {
		hook := e.afterRequest
		e.afterRequest = nil
		hook(ctx)
	}
	return request, found, err
}

func (e *approvalClosureEvidence) ReadAgentResponse(ctx context.Context, stream, subject string) (retainedLoopMessage, bool, error) {
	response, found, err := e.loopSettlementEvidenceReader.ReadAgentResponse(ctx, stream, subject)
	if e.afterResponse != nil {
		hook := e.afterResponse
		e.afterResponse = nil
		hook(ctx)
	}
	return response, found, err
}

// spec: agentic-loop / Approval-required tool statuses settle by observed execution phase
func TestNewApprovalGateCannotOverwriteCancellationAfterObservation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	f := newApprovalRecoveryFixture(t)
	require.NoError(t, f.entity.ResolveApproval())
	f.entity.PendingToolResults = nil
	f.bucket.values[f.entity.ID] = settlementLoopRecord(t, f.entity)
	require.NoError(t, f.c.handler.loopManager.restoreLoopFromRequest(f.entity, f.request, nil))
	bucket := &approvalGateCommitBucket{approvalRevisionBucket: f.c.loopsBucket.(*approvalRevisionBucket)}
	f.c.loopsBucket = bucket
	// A disconnected client makes attempted publication fail at the existing
	// unit seam. This seeded owner-ordering proof does not claim native PubAck
	// or full cancellation settlement; those have separate integration gates.
	f.c.natsClient = &natsclient.Client{}
	probe := newTerminalReaderProbe(f.c, f.entity.ID)
	var committed []byte
	var committedRevision uint64
	f.c.settlementEvidence = &approvalClosureEvidence{
		loopSettlementEvidenceReader: f.evidence,
		afterResponse: func(ctx context.Context) {
			// Tool-result and cancellation inputs have distinct consumer owners.
			// The tool callback already observed revision 1, but has not restored
			// its batch. Commit cancellation through the same current-state
			// primitives used by handleCancelSignal before allowing it to resume.
			require.Equal(t, uint64(1), bucket.revisions[f.entity.ID])
			closed, err := f.c.handler.CancelLoop(f.entity.ID, "reviewer")
			require.NoError(t, err)
			require.Equal(t, agentic.LoopStateCancelled, closed.State)
			require.NoError(t, f.c.persistLoopState(ctx, f.entity.ID))
			committed = append([]byte(nil), f.bucket.values[f.entity.ID]...)
			committedRevision = bucket.revisions[f.entity.ID]
			require.Equal(t, uint64(2), committedRevision)
			f.c.releaseLoopTransientState(f.entity.ID)
		},
	}

	decision, callbackErr := f.c.handleToolResultMessage(ctx, settlementEnvelope(t, &f.result))

	require.NotEmpty(t, committed, "the cancellation must really commit during the observed read")
	assert.Equal(t, natsclient.DeliveryDecisionRetry, decision)
	assert.ErrorContains(t, callbackErr, "commit new approval gate: KV revision mismatch")
	assert.Equal(t, 1, bucket.updates)
	assert.Equal(t, uint64(1), bucket.usedRevision)
	assert.Equal(t, 1, bucket.puts, "only cancellation writes unconditionally; the stale gate cannot fall back to Put")
	assert.Equal(t, committed, f.bucket.values[f.entity.ID], "stale restoration must not overwrite committed cancellation")
	assert.Equal(t, committedRevision, bucket.revisions[f.entity.ID])
	var durable agentic.LoopEntity
	require.NoError(t, json.Unmarshal(f.bucket.values[f.entity.ID], &durable))
	assert.Equal(t, agentic.LoopStateCancelled, durable.State)
	assert.Equal(t, agentic.OutcomeCancelled, durable.Outcome)
	assert.Nil(t, durable.PendingApproval)
	assert.Empty(t, probe.facts, "lost new-gate CAS must precede all prompt/audit consequences")
	assert.Empty(t, f.c.handler.loopManager.loops)
	assert.Empty(t, perLoopMapCount(f.c.handler.loopManager, f.entity.ID))
	assert.Empty(t, f.c.handler.trajectoryManager.trajectories)
	t.Logf("cancellation revision=%d stale gate revision=%d decision=%v err=%v",
		committedRevision, bucket.usedRevision, decision, callbackErr)
}

// spec: agentic-loop / LoopEntity has one operational state contract
func TestRestorationPreservesProtectedCurrentEntry(t *testing.T) {
	for _, protected := range []string{"terminal", "approval"} {
		for _, install := range []string{"restore", "update"} {
			t.Run(protected+"/"+install, func(t *testing.T) {
				f := newApprovalRecoveryFixture(t)
				stale := f.entity
				require.NoError(t, stale.ResolveApproval())
				m := f.c.handler.loopManager
				require.NoError(t, m.restoreLoopFromRequest(stale, f.request, nil))
				if protected == "terminal" {
					_, err := m.CancelLoop(stale.ID, "operator")
					require.NoError(t, err)
				} else {
					require.NoError(t, m.UpdateLoop(f.entity))
				}
				before, err := m.GetLoop(stale.ID)
				require.NoError(t, err)
				if install == "restore" {
					err = m.restoreLoopFromRequest(stale, f.request, nil)
				} else {
					err = m.UpdateLoop(stale)
				}
				require.Error(t, err)
				after, err := m.GetLoop(stale.ID)
				require.NoError(t, err)
				require.Equal(t, before, after)
			})
		}
	}
}
