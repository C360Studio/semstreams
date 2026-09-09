package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type approvalRecoveryFixture struct {
	c        *Component
	entity   agentic.LoopEntity
	request  agentic.AgentRequest
	response agentic.AgentResponse
	result   agentic.ToolResult
	approval agentic.ApprovalResponse
	bucket   *settlementBucket
	evidence *settlementEvidence
}

type approvalPutHookBucket struct {
	jetstream.KeyValue
	beforePut func(context.Context, string, []byte) error
}

type approvalTrajectoryHookBucket struct {
	trajectoryFactBucket
	beforeCreate func(context.Context, []byte) error
}

func (b *approvalTrajectoryHookBucket) Create(ctx context.Context, key string, data []byte, opts ...jetstream.KVCreateOpt) (uint64, error) {
	if err := b.beforeCreate(ctx, data); err != nil {
		return 0, err
	}
	return b.trajectoryFactBucket.Create(ctx, key, data, opts...)
}

func (b *approvalPutHookBucket) Put(ctx context.Context, key string, data []byte) (uint64, error) {
	if err := b.beforePut(ctx, key, data); err != nil {
		return 0, err
	}
	return b.KeyValue.Put(ctx, key, data)
}

func (b *approvalPutHookBucket) Update(ctx context.Context, key string, data []byte, revision uint64) (uint64, error) {
	if err := b.beforePut(ctx, key, data); err != nil {
		return 0, err
	}
	return b.KeyValue.Update(ctx, key, data, revision)
}

type approvalRevisionEntry struct {
	jetstream.KeyValueEntry
	revision uint64
}

func (e approvalRevisionEntry) Revision() uint64 { return e.revision }

// Model the native per-key revision precondition, including writes performed
// by the independent result owner through Put, not just approval Update calls.
type approvalRevisionBucket struct {
	*settlementBucket
	revisions map[string]uint64
}

func (b *approvalRevisionBucket) Get(ctx context.Context, key string) (jetstream.KeyValueEntry, error) {
	entry, err := b.settlementBucket.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if b.revisions[key] == 0 {
		b.revisions[key] = 1
	}
	return approvalRevisionEntry{KeyValueEntry: entry, revision: b.revisions[key]}, nil
}

func (b *approvalRevisionBucket) Put(ctx context.Context, key string, data []byte) (uint64, error) {
	if _, err := b.settlementBucket.Put(ctx, key, data); err != nil {
		return 0, err
	}
	b.revisions[key]++
	return b.revisions[key], nil
}

func (b *approvalRevisionBucket) Update(ctx context.Context, key string, data []byte, revision uint64) (uint64, error) {
	if b.revisions[key] != revision {
		return 0, errors.New("KV revision mismatch")
	}
	return b.Put(ctx, key, data)
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
func TestApprovalFinalPutCannotOverwriteCompletedToolResult(t *testing.T) {
	f := newApprovalRecoveryFixture(t)
	interleaved := false
	f.c.loopsBucket = &approvalPutHookBucket{KeyValue: f.c.loopsBucket, beforePut: func(ctx context.Context, key string, data []byte) error {
		if key != f.entity.ID || interleaved {
			return nil
		}
		var approvalSnapshot agentic.LoopEntity
		require.NoError(t, json.Unmarshal(data, &approvalSnapshot))
		require.Nil(t, approvalSnapshot.PendingApproval)
		require.Equal(t, agentic.LoopStateExecuting, approvalSnapshot.State)
		interleaved = true
		// The approval owner commits its pre-publication JSON after publication.
		// Complete the independent normal ToolResult owner before that write lands.
		completed := f.result
		completed.Error, completed.ErrorKind, completed.Content = "", "", "deleted"
		completed.StopLoop = true
		decision, err := f.c.handleToolResultMessage(ctx, settlementEnvelope(t, &completed))
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
		var terminal agentic.LoopEntity
		require.NoError(t, json.Unmarshal(f.bucket.values[f.entity.ID], &terminal))
		require.Equal(t, agentic.LoopStateComplete, terminal.State)
		require.Contains(t, f.bucket.values, "COMPLETE_"+f.entity.ID)
		require.Empty(t, f.c.handler.loopManager.loops, "the completed result owner must have released its process state")
		return nil
	}}
	decision, err := f.c.handleApprovalResponseMessage(t.Context(), settlementEnvelope(t, &f.approval))
	require.ErrorContains(t, err, "KV revision mismatch")
	require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
	require.True(t, interleaved)
	var final agentic.LoopEntity
	require.NoError(t, json.Unmarshal(f.bucket.values[f.entity.ID], &final))
	require.Equal(t, agentic.LoopStateComplete, final.State, "late approval snapshot must not overwrite the completed result owner's terminal marker")
	require.Equal(t, "deleted", final.Result)
	require.Nil(t, final.PendingApproval)
	require.Contains(t, f.bucket.values, "COMPLETE_"+f.entity.ID)
	require.Empty(t, f.c.handler.loopManager.loops)
	// The existing nil-client unit seam does not establish native PubAck.
	// This test isolates ordering of the ordinary owners' real persistence code.
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
func TestApprovalCommitDoesNotBorrowUncommittedTerminalState(t *testing.T) {
	f := newApprovalRecoveryFixture(t)
	started := false
	var terminalResult HandlerResult
	completed := f.result
	completed.Error, completed.ErrorKind, completed.Content = "", "", "deleted"
	completed.StopLoop = true
	audit := &approvalTrajectoryHookBucket{
		trajectoryFactBucket: &trajectoryTestBucket{values: make(map[string][]byte)},
		beforeCreate: func(ctx context.Context, data []byte) error {
			var fact agentic.TrajectoryFactV1
			if err := json.Unmarshal(data, &fact); err != nil {
				return err
			}
			if fact.Kind != agentic.TrajectoryKindToolRequested {
				return nil
			}
			started = true
			// Stop at the ordinary owner's boundary between process transition
			// and persistHandlerResult: no required terminal effect has run yet.
			var err error
			terminalResult, err = f.c.handler.HandleToolResult(ctx, f.entity.ID, completed)
			return err
		},
	}
	f.c.trajectoryRecorder = newTrajectoryRecorder(audit, nil, "objectstore", func(trajectoryAuditFailure) {})
	decision, err := f.c.handleApprovalResponseMessage(t.Context(), settlementEnvelope(t, &f.approval))
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	require.True(t, started)
	process, err := f.c.handler.GetLoop(f.entity.ID)
	require.NoError(t, err)
	require.Equal(t, agentic.LoopStateComplete, process.State)
	require.NotContains(t, f.bucket.values, "COMPLETE_"+f.entity.ID, "required result effects are still blocked")
	var durable agentic.LoopEntity
	require.NoError(t, json.Unmarshal(f.bucket.values[f.entity.ID], &durable))
	require.Equal(t, agentic.LoopStateExecuting, durable.State, "approval must commit its own pre-publication snapshot, never another owner's uncommitted terminal state")
	require.Nil(t, durable.PendingApproval)
	require.NoError(t, f.c.persistHandlerResult(t.Context(), terminalResult))
	require.NoError(t, json.Unmarshal(f.bucket.values[f.entity.ID], &durable))
	require.Equal(t, agentic.LoopStateComplete, durable.State)
	require.Contains(t, f.bucket.values, "COMPLETE_"+f.entity.ID)
	// The test-only audit seam schedules the normal result transition.
	// This nil-client unit proof isolates snapshot ordering, not server PubAck.
}

func newApprovalRecoveryFixture(t *testing.T) approvalRecoveryFixture {
	t.Helper()
	loopID := uuid.NewString()
	requestID := loopID + ":req:" + uuid.NewString()
	calls := []agentic.ToolCall{{ID: "provider-call", Name: "delete_rule",
		Arguments: map[string]any{"rule_id": "rule-42"}, TraceID: "trace-original"}}
	require.NoError(t, stampToolExecutionCorrelation(requestID, calls))
	result := agentic.ToolResult{LoopID: loopID, RequestID: requestID, ExecutionID: calls[0].ExecutionID,
		CallID: calls[0].ID, CallOrdinal: calls[0].CallOrdinal, Name: calls[0].Name, TraceID: calls[0].TraceID,
		ErrorKind: agentic.ToolErrorPermission, Error: agentic.ApprovalRequiredPrefix + "review required"}
	entity := agentic.NewLoopEntity(loopID, "approval-task", "general", "model", 3)
	entity.State = agentic.LoopStateExecuting
	require.NoError(t, entity.BeginAwaitingApproval(result.CallID, result.Name, calls[0].Arguments,
		result.Error, time.Hour, result.TraceID))
	entity.PendingApproval.RequestID = requestID
	entity.PendingApproval.ExecutionID = result.ExecutionID
	entity.PendingApproval.CallOrdinal = result.CallOrdinal
	entity.PendingToolResults = map[string]agentic.ToolResult{result.ExecutionID: result}
	request := agentic.AgentRequest{LoopID: loopID, RequestID: requestID, Role: entity.Role, Model: entity.Model,
		Messages: []agentic.ChatMessage{{Role: "system", Content: "be precise"}, {Role: "user", Content: "delete rule-42"}},
		Tools:    []agentic.ToolDefinition{{Name: "delete_rule", Parameters: map[string]any{"type": "object"}}}}
	response := agentic.AgentResponse{RequestID: requestID, Status: agentic.StatusToolCall,
		Message: agentic.ChatMessage{Role: "assistant", ToolCalls: calls}}
	bucket := &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, entity)}}
	evidence := &settlementEvidence{
		request: retainedLoopMessage{subject: "agent.request." + loopID, data: settlementEnvelope(t, &request)}, requestFound: true,
		response: retainedLoopMessage{subject: "agent.response." + requestID, data: settlementEnvelope(t, &response)}, responseFound: true,
	}
	c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
	c.loopsBucket = &approvalRevisionBucket{settlementBucket: bucket, revisions: make(map[string]uint64)}
	c.settlementEvidence = evidence
	return approvalRecoveryFixture{c: c, entity: entity, request: request, response: response, result: result,
		approval: agentic.ApprovalResponse{LoopID: loopID, CallID: result.CallID, Decision: agentic.ApprovalDecisionApprove,
			ApprovedBy: "reviewer", DecidedAt: time.Now().UTC()}, bucket: bucket, evidence: evidence}
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
func TestColdApprovalRestoresCurrentBatch(t *testing.T) {
	f := newApprovalRecoveryFixture(t)
	decision, err := f.c.handleApprovalResponseMessage(t.Context(), settlementEnvelope(t, &f.approval))
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	var durable agentic.LoopEntity
	require.NoError(t, json.Unmarshal(f.bucket.values[f.entity.ID], &durable))
	require.Nil(t, durable.PendingApproval, "cold approval must actually resolve, not silently ACK a missing map")
	require.Equal(t, f.entity.StateBeforeApproval, durable.State)
	require.Equal(t, f.entity.TaskID, durable.TaskID)
	require.Equal(t, f.result, durable.PendingToolResults[f.result.ExecutionID])
	loopID, found := f.c.handler.loopManager.GetLoopForToolCall(f.result.ExecutionID)
	require.True(t, found)
	require.Equal(t, f.entity.ID, loopID)
	require.Equal(t, f.request.Tools, f.c.handler.loopManager.GetCachedTools(loopID))
	wantContext := append(append([]agentic.ChatMessage(nil), f.request.Messages...), f.response.Message)
	require.Equal(t, wantContext, f.c.handler.GetContextManager(loopID).GetContext())
	// The component's existing nil-client unit seam does not prove PubAck;
	// the unchanged started-owner integration is the real publication gate.
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
func TestApprovalRedispatchPreservesOriginalTrace(t *testing.T) {
	f := newApprovalRecoveryFixture(t)
	require.NoError(t, f.c.handler.loopManager.restoreToolBatch(f.entity, f.request, f.response, f.result))
	result, err := f.c.handler.HandleApprovalResponse(t.Context(), f.approval)
	require.NoError(t, err)
	require.Len(t, result.PublishedMessages, 1)
	base, err := f.c.decoder.Decode(result.PublishedMessages[0].Data)
	require.NoError(t, err)
	call, ok := base.Payload().(*agentic.ToolCall)
	require.True(t, ok)
	require.Equal(t, f.result.TraceID, call.TraceID)
	require.Equal(t, f.result.LoopID, call.LoopID)
	require.Equal(t, f.result.RequestID, call.RequestID)
	require.Equal(t, f.result.ExecutionID, call.ExecutionID)
	require.Equal(t, f.result.CallOrdinal, call.CallOrdinal)
	require.Equal(t, f.response.Message.ToolCalls[0].Arguments, call.Arguments)
	require.Equal(t, f.approval.ApprovedBy, call.ApprovedBy)
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
func TestColdApprovalUnresolvedOrConflictingEvidenceDoesNotResolve(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*approvalRecoveryFixture)
		want   natsclient.DeliveryDecision
	}{
		{"request unavailable", func(f *approvalRecoveryFixture) { f.evidence.requestErr = errors.New("request unavailable") }, natsclient.DeliveryDecisionRetry},
		{"response visibility unresolved", func(f *approvalRecoveryFixture) { f.evidence.responseFound = false }, natsclient.DeliveryDecisionRetry},
		{"cold nonpending is not applied proof", func(f *approvalRecoveryFixture) {
			f.entity.State, f.entity.PendingApproval = agentic.LoopStateExecuting, nil
		}, natsclient.DeliveryDecisionRetry},
		{"gated result missing", func(f *approvalRecoveryFixture) { f.entity.PendingToolResults = nil }, natsclient.DeliveryDecisionRetry},
		{"pending arguments conflict", func(f *approvalRecoveryFixture) {
			f.entity.PendingApproval.Arguments = map[string]any{"rule_id": "different"}
		}, natsclient.DeliveryDecisionQuarantine},
		{"duplicate current provider call", func(f *approvalRecoveryFixture) {
			f.response.Message.ToolCalls = append(f.response.Message.ToolCalls, f.response.Message.ToolCalls[0])
		}, natsclient.DeliveryDecisionQuarantine},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newApprovalRecoveryFixture(t)
			tc.change(&f)
			f.bucket.values[f.entity.ID] = settlementLoopRecord(t, f.entity)
			f.evidence.response.data = settlementEnvelope(t, &f.response)
			before := append([]byte(nil), f.bucket.values[f.entity.ID]...)
			decision, err := f.c.handleApprovalResponseMessage(t.Context(), settlementEnvelope(t, &f.approval))
			require.Error(t, err)
			require.Equal(t, tc.want, decision)
			require.Equal(t, before, f.bucket.values[f.entity.ID])
			require.Empty(t, f.c.handler.loopManager.loops, "unproven approval cannot install speculative process state")
		})
	}
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
func TestColdApprovalPublicationFailurePreservesPending(t *testing.T) {
	f := newApprovalRecoveryFixture(t)
	f.c.natsClient = &natsclient.Client{} // Disconnected real publisher: no PubAck.
	before := append([]byte(nil), f.bucket.values[f.entity.ID]...)
	decision, err := f.c.handleApprovalResponseMessage(t.Context(), settlementEnvelope(t, &f.approval))
	require.ErrorContains(t, err, "publish result")
	require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
	require.Equal(t, before, f.bucket.values[f.entity.ID], "required PubAck must precede clearing durable pending state")
	require.Empty(t, f.c.handler.loopManager.loops, "retry must not see speculative resolved process state")
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
func TestColdApprovalFinalStateFailureRetries(t *testing.T) {
	f := newApprovalRecoveryFixture(t)
	f.bucket.putErr = errors.New("pending state write failed")
	before := append([]byte(nil), f.bucket.values[f.entity.ID]...)
	decision, err := f.c.handleApprovalResponseMessage(t.Context(), settlementEnvelope(t, &f.approval))
	require.ErrorIs(t, err, f.bucket.putErr)
	require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
	require.Equal(t, before, f.bucket.values[f.entity.ID])
	require.Empty(t, f.c.handler.loopManager.loops)
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
func TestColdApprovalPreservesLiveDiscardOfSiblingCalls(t *testing.T) {
	f := newApprovalRecoveryFixture(t)
	f.response.Message.ToolCalls = append(f.response.Message.ToolCalls, agentic.ToolCall{ID: "sibling-call", Name: "search"})
	beforeGate := f.entity
	beforeGate.State, beforeGate.StateBeforeApproval = agentic.LoopStateExecuting, ""
	beforeGate.PendingApproval, beforeGate.PendingToolResults = nil, nil
	require.NoError(t, f.c.handler.loopManager.restoreLoopFromRequest(beforeGate, f.request, nil))
	_, err := f.c.handler.trajectoryManager.startTrajectory(f.entity.ID)
	require.NoError(t, err)
	_, err = f.c.handler.HandleModelResponse(t.Context(), f.entity.ID, f.response)
	require.NoError(t, err)
	_, err = f.c.handler.HandleToolResult(t.Context(), f.entity.ID, f.result)
	require.NoError(t, err)
	checkpoint, err := f.c.handler.GetLoop(f.entity.ID)
	require.NoError(t, err)
	cold := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
	cold.loopsBucket = &approvalRevisionBucket{
		settlementBucket: &settlementBucket{values: map[string][]byte{checkpoint.ID: settlementLoopRecord(t, checkpoint)}},
		revisions:        make(map[string]uint64),
	}
	f.evidence.response.data = settlementEnvelope(t, &f.response)
	cold.settlementEvidence = f.evidence
	for name, c := range map[string]*Component{"live": f.c, "cold": cold} {
		t.Run(name, func(t *testing.T) {
			if name == "live" {
				_, err := c.handler.HandleApprovalResponse(t.Context(), f.approval)
				require.NoError(t, err)
			} else {
				decision, err := c.handleApprovalResponseMessage(t.Context(), settlementEnvelope(t, &f.approval))
				require.NoError(t, err)
				require.Equal(t, natsclient.DeliveryDecisionAck, decision)
			}
			completed := f.result
			completed.Error, completed.ErrorKind, completed.Content = "", "", "approved deletion"
			result, err := c.handler.HandleToolResult(t.Context(), f.entity.ID, completed)
			require.NoError(t, err)
			require.Len(t, result.PublishedMessages, 1)
			require.Equal(t, "agent.request."+f.entity.ID, result.PublishedMessages[0].Subject,
				"approval completion must ask the model again, not revive the discarded sibling execution")
		})
	}
}
