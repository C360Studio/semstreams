package agenticloop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type approvalGateCommitBucket struct {
	*approvalRevisionBucket
	puts, updates int
	conflict      bool
	usedRevision  uint64
}

func (b *approvalGateCommitBucket) Put(ctx context.Context, key string, data []byte) (uint64, error) {
	b.puts++
	return b.approvalRevisionBucket.Put(ctx, key, data)
}

func (b *approvalGateCommitBucket) Update(ctx context.Context, key string, data []byte, revision uint64) (uint64, error) {
	b.updates++
	b.usedRevision = revision
	if b.conflict {
		// A competing native write advances this exact key's revision, even
		// when its bytes are unchanged. The real Update precondition must lose.
		if _, err := b.approvalRevisionBucket.Put(ctx, key, b.values[key]); err != nil {
			return 0, err
		}
	}
	return b.approvalRevisionBucket.Update(ctx, key, data, revision)
}

// spec: agentic-loop / Approval-required tool statuses settle by observed execution phase
func TestApprovalRequiredMatchingPromptRetriesWithoutRewritingGate(t *testing.T) {
	for _, route := range []string{"warm", "cold"} {
		t.Run(route, func(t *testing.T) {
			f := newApprovalRecoveryFixture(t)
			if route == "warm" {
				require.NoError(t, f.c.handler.loopManager.restoreToolBatch(f.entity, f.request, f.response, f.result))
			}
			before := append([]byte(nil), f.bucket.values[f.entity.ID]...)
			beforeProcess, err := json.Marshal(f.c.handler.loopManager.loops)
			require.NoError(t, err)
			probe := newTerminalReaderProbe(f.c, f.entity.ID)
			f.c.natsClient = &natsclient.Client{}
			decision, callbackErr := f.c.handleToolResultMessage(t.Context(), settlementEnvelope(t, &f.result))
			assert.ErrorContains(t, callbackErr, "publish result agent.approval_pending."+f.entity.ID)
			assert.Equal(t, natsclient.DeliveryDecisionRetry, decision)
			assert.Equal(t, before, f.bucket.values[f.entity.ID], "retry must preserve the original gate and deadline")
			afterProcess, err := json.Marshal(f.c.handler.loopManager.loops)
			require.NoError(t, err)
			assert.Equal(t, beforeProcess, afterProcess)
			assert.Empty(t, probe.facts)

			// Existing nil-client seam proves only callback continuation. The
			// native gate must independently prove actual pending bytes and PubAck.
			f.c.natsClient = nil
			decision, callbackErr = f.c.handleToolResultMessage(t.Context(), settlementEnvelope(t, &f.result))
			assert.NoError(t, callbackErr)
			assert.Equal(t, natsclient.DeliveryDecisionAck, decision)
			assert.Equal(t, before, f.bucket.values[f.entity.ID])
			assert.Empty(t, probe.facts)
		})
	}
}

// spec: agentic-loop / Approval-required tool statuses settle by observed execution phase
func TestNewApprovalGateUsesObservedRevisionBeforePrompt(t *testing.T) {
	for _, mode := range []string{"publication fails", "revision conflicts"} {
		t.Run(mode, func(t *testing.T) {
			f := newApprovalRecoveryFixture(t)
			// Arrange the current executing batch before any result has entered
			// its gate. These are seeded unit records, not a fabricated delivery.
			require.NoError(t, f.entity.ResolveApproval())
			f.entity.PendingToolResults = nil
			f.bucket.values[f.entity.ID] = settlementLoopRecord(t, f.entity)
			before := append([]byte(nil), f.bucket.values[f.entity.ID]...)
			bucket := &approvalGateCommitBucket{approvalRevisionBucket: f.c.loopsBucket.(*approvalRevisionBucket),
				conflict: mode == "revision conflicts"}
			f.c.loopsBucket = bucket
			f.c.natsClient = &natsclient.Client{}
			probe := newTerminalReaderProbe(f.c, f.entity.ID)

			decision, callbackErr := f.c.handleToolResultMessage(t.Context(), settlementEnvelope(t, &f.result))

			assert.Equal(t, natsclient.DeliveryDecisionRetry, decision)
			assert.Equal(t, 1, bucket.updates)
			assert.Zero(t, bucket.puts, "new gate never falls back to unconditional Put")
			assert.Equal(t, uint64(1), bucket.usedRevision)
			if bucket.conflict {
				assert.ErrorContains(t, callbackErr, "KV revision mismatch")
				assert.Equal(t, before, f.bucket.values[f.entity.ID])
				assert.Empty(t, probe.facts, "failed conditional gate must publish/record no speculative consequence")
			} else {
				assert.ErrorContains(t, callbackErr, "publish result agent.approval_pending."+f.entity.ID)
				var pending agentic.LoopEntity
				require.NoError(t, json.Unmarshal(f.bucket.values[f.entity.ID], &pending))
				require.NotNil(t, pending.PendingApproval)
				assert.Equal(t, f.result.ExecutionID, pending.PendingApproval.ExecutionID)
				assert.Equal(t, f.result, pending.PendingToolResults[f.result.ExecutionID])
			}
			assert.Empty(t, f.c.handler.loopManager.loops, "failed attempt releases speculative process state")
		})
	}
}

// spec: agentic-loop / Approval-required tool statuses settle by observed execution phase
func TestApprovalRequiredReplayIsSupersededBeforeMutation(t *testing.T) {
	for _, route := range []string{"warm", "cold"} {
		t.Run(route, func(t *testing.T) {
			f := newApprovalRecoveryFixture(t)
			original := settlementEnvelope(t, &f.result)
			decision, err := f.c.handleApprovalResponseMessage(t.Context(), settlementEnvelope(t, &f.approval))
			require.NoError(t, err)
			require.Equal(t, natsclient.DeliveryDecisionAck, decision)
			var closed agentic.LoopEntity
			require.NoError(t, json.Unmarshal(f.bucket.values[f.entity.ID], &closed))
			require.Nil(t, closed.PendingApproval)
			require.Equal(t, f.result, closed.PendingToolResults[f.result.ExecutionID])
			if route == "cold" {
				old := f.c
				f.c = releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
				f.c.loopsBucket, f.c.settlementEvidence = old.loopsBucket, old.settlementEvidence
				require.Empty(t, f.c.handler.loopManager.loops)
			}
			before, err := json.Marshal(f.bucket.values)
			require.NoError(t, err)
			beforeProcess, err := json.Marshal(f.c.handler.loopManager.loops)
			require.NoError(t, err)
			beforePending := f.c.handler.loopManager.GetPendingTools(f.entity.ID)
			beforeMaps := perLoopMapCount(f.c.handler.loopManager, f.entity.ID)
			beforeTrajectory, err := json.Marshal(f.c.handler.trajectoryManager.trajectories)
			require.NoError(t, err)
			probe := newTerminalReaderProbe(f.c, f.entity.ID)
			var logs bytes.Buffer
			f.c.logger = slog.New(slog.NewTextHandler(&logs, nil))
			f.c.metrics = getMetrics(nil)
			supersededBefore := testutil.ToFloat64(f.c.metrics.approvalStatusesSuperseded)
			// A disconnected client makes any attempted business publication fail;
			// this unit callback does not claim a server PubAck or native delivery.
			f.c.natsClient = &natsclient.Client{}

			decision, callbackErr := f.c.handleToolResultMessage(t.Context(), original)

			after, err := json.Marshal(f.bucket.values)
			require.NoError(t, err)
			afterProcess, err := json.Marshal(f.c.handler.loopManager.loops)
			require.NoError(t, err)
			afterTrajectory, err := json.Marshal(f.c.handler.trajectoryManager.trajectories)
			require.NoError(t, err)
			t.Logf("route=%s execution=%s decision=%v err=%v audit_facts=%d", route,
				f.result.ExecutionID, decision, callbackErr, len(probe.facts))
			assert.NoError(t, callbackErr)
			assert.Equal(t, natsclient.DeliveryDecisionAck, decision)
			assert.Equal(t, before, after, "superseded gate phase cannot rewrite durable authority")
			assert.Equal(t, beforeProcess, afterProcess, "classify before changing process state")
			assert.Equal(t, beforePending, f.c.handler.loopManager.GetPendingTools(f.entity.ID))
			assert.Equal(t, beforeMaps, perLoopMapCount(f.c.handler.loopManager, f.entity.ID))
			assert.Equal(t, beforeTrajectory, afterTrajectory)
			assert.Empty(t, probe.facts, "superseded status must not record a second business trajectory fact")
			assert.Contains(t, logs.String(), "approval-required tool status superseded by observed execution phase")
			assert.Contains(t, logs.String(), "loop_id="+f.entity.ID)
			assert.Contains(t, logs.String(), "execution_id="+f.result.ExecutionID)
			assert.Equal(t, supersededBefore+1, testutil.ToFloat64(f.c.metrics.approvalStatusesSuperseded))
		})
	}
}

// spec: agentic-loop / Approval-required tool statuses settle by observed execution phase
func TestSupersededApprovalStatusRequiresOptionalCallCorrelation(t *testing.T) {
	for _, phase := range []string{"closed gate", "post-gate result"} {
		for _, fields := range []string{"matching", "empty", "wrong loop", "wrong trace"} {
			t.Run(phase+"/"+fields, func(t *testing.T) {
				f := newApprovalRecoveryFixture(t)
				decision, err := f.c.handleApprovalResponseMessage(t.Context(), settlementEnvelope(t, &f.approval))
				require.NoError(t, err)
				require.Equal(t, natsclient.DeliveryDecisionAck, decision)
				if phase == "post-gate result" {
					// Seed the alternate positive-evidence branch in this unit
					// callback proof; this is not a native completion history.
					var closed agentic.LoopEntity
					require.NoError(t, json.Unmarshal(f.bucket.values[f.entity.ID], &closed))
					completed := f.result
					completed.Error, completed.ErrorKind, completed.Content = "", "", "deleted"
					closed.PendingToolResults[completed.ExecutionID] = completed
					f.bucket.values[f.entity.ID] = settlementLoopRecord(t, closed)
				}
				call := &f.response.Message.ToolCalls[0]
				call.LoopID, call.TraceID = f.entity.ID, f.result.TraceID
				switch fields {
				case "empty":
					call.LoopID, call.TraceID = "", ""
				case "wrong loop":
					call.LoopID = uuid.NewString()
				case "wrong trace":
					call.TraceID = "conflicting-trace"
				}
				require.NoError(t, f.response.Validate())
				f.evidence.response.data = settlementEnvelope(t, &f.response)
				before := append([]byte(nil), f.bucket.values[f.entity.ID]...)
				beforeProcess, err := json.Marshal(f.c.handler.loopManager.loops)
				require.NoError(t, err)
				probe := newTerminalReaderProbe(f.c, f.entity.ID)
				f.c.natsClient = &natsclient.Client{}

				decision, err = f.c.handleToolResultMessage(t.Context(), settlementEnvelope(t, &f.result))

				if fields == "wrong loop" || fields == "wrong trace" {
					assert.Error(t, err)
					assert.True(t, errs.IsFatal(err))
					assert.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
				} else {
					assert.NoError(t, err)
					assert.Equal(t, natsclient.DeliveryDecisionAck, decision)
				}
				afterProcess, marshalErr := json.Marshal(f.c.handler.loopManager.loops)
				require.NoError(t, marshalErr)
				assert.Equal(t, beforeProcess, afterProcess)
				assert.Equal(t, before, f.bucket.values[f.entity.ID])
				assert.Empty(t, probe.facts)
			})
		}
	}
}

// spec: agentic-loop / Approval-required tool statuses settle by observed execution phase
func TestApprovalRequiredSiblingCannotBeAbsorbedAsConsumedGate(t *testing.T) {
	for _, route := range []string{"warm", "cold"} {
		t.Run(route, func(t *testing.T) {
			f := newApprovalRecoveryFixture(t)
			f.response.Message.ToolCalls = append(f.response.Message.ToolCalls, agentic.ToolCall{
				ID: "provider-sibling", Name: "delete_rule", Arguments: map[string]any{"rule_id": "rule-99"},
			})
			require.NoError(t, stampToolExecutionCorrelation(f.request.RequestID, f.response.Message.ToolCalls))
			call := f.response.Message.ToolCalls[1]
			sibling := agentic.ToolResult{LoopID: f.entity.ID, RequestID: call.RequestID, ExecutionID: call.ExecutionID,
				CallID: call.ID, CallOrdinal: call.CallOrdinal, Name: call.Name,
				ErrorKind: agentic.ToolErrorPermission, Error: agentic.ApprovalRequiredPrefix + "review sibling"}
			require.NoError(t, sibling.Validate())
			require.NoError(t, f.response.Validate())
			require.Equal(t, f.result.RequestID, sibling.RequestID)
			require.NotEqual(t, f.result.ExecutionID, sibling.ExecutionID)
			require.Equal(t, uint32(2), sibling.CallOrdinal)
			f.evidence.response.data = settlementEnvelope(t, &f.response)
			// The current gate and preceding result belong to A. B is a distinct,
			// valid tuple from that exact same retained batch, never an old gate.
			if route == "warm" {
				require.NoError(t, f.c.handler.loopManager.restoreToolBatch(f.entity, f.request, f.response, sibling))
				_, err := f.c.handler.trajectoryManager.startTrajectory(f.entity.ID)
				require.NoError(t, err)
			}
			before, err := json.Marshal(f.bucket.values)
			require.NoError(t, err)
			beforeProcess, err := json.Marshal(f.c.handler.loopManager.loops)
			require.NoError(t, err)
			beforePending := f.c.handler.loopManager.GetPendingTools(f.entity.ID)
			beforeMaps := perLoopMapCount(f.c.handler.loopManager, f.entity.ID)
			beforeTrajectory, err := json.Marshal(f.c.handler.trajectoryManager.trajectories)
			require.NoError(t, err)
			probe := newTerminalReaderProbe(f.c, f.entity.ID)
			f.c.natsClient = &natsclient.Client{}

			decision, callbackErr := f.c.handleToolResultMessage(t.Context(), settlementEnvelope(t, &sibling))

			after, err := json.Marshal(f.bucket.values)
			require.NoError(t, err)
			afterProcess, err := json.Marshal(f.c.handler.loopManager.loops)
			require.NoError(t, err)
			afterTrajectory, err := json.Marshal(f.c.handler.trajectoryManager.trajectories)
			require.NoError(t, err)
			var current agentic.LoopEntity
			require.NoError(t, json.Unmarshal(f.bucket.values[f.entity.ID], &current))
			t.Logf("route=%s pending_execution=%s sibling_execution=%s decision=%v err=%v sibling_stored=%t audit_facts=%d",
				route, f.result.ExecutionID, sibling.ExecutionID, decision, callbackErr,
				current.PendingToolResults[sibling.ExecutionID].ExecutionID != "", len(probe.facts))
			// Do not prescribe an unruled retry/quarantine subtype: the measured
			// boundary is explicit refusal before B can look like a consumed gate.
			assert.Error(t, callbackErr)
			assert.NotEqual(t, natsclient.DeliveryDecisionAck, decision)
			assert.Equal(t, before, after, "B must not become retained approval evidence while A owns the gate")
			assert.Equal(t, beforeProcess, afterProcess)
			assert.Equal(t, beforePending, f.c.handler.loopManager.GetPendingTools(f.entity.ID))
			assert.Equal(t, beforeMaps, perLoopMapCount(f.c.handler.loopManager, f.entity.ID))
			assert.Equal(t, beforeTrajectory, afterTrajectory)
			assert.Empty(t, probe.facts)
		})
	}
}

func approvalGateSettlementFixture(t *testing.T) (*Component, *agentic.ToolResult) {
	t.Helper()
	handler := NewMessageHandler(DefaultConfig())
	c := releaseTestComponent(t, handler)
	taskResult, err := handler.HandleTask(t.Context(), TaskMessage{
		LoopID: uuid.NewString(), TaskID: "task-approval-settlement", Role: "general", Model: "model", Prompt: "delete rule-42",
	})
	require.NoError(t, err)
	require.Len(t, taskResult.PublishedMessages, 2)
	requestWire := taskResult.PublishedMessages[0]
	require.Equal(t, "agent.request."+taskResult.LoopID, requestWire.Subject)
	requestMsg, err := c.decoder.Decode(requestWire.Data)
	require.NoError(t, err)
	request, ok := requestMsg.Payload().(*agentic.AgentRequest)
	require.True(t, ok)
	response := agentic.AgentResponse{
		RequestID: request.RequestID, Status: agentic.StatusToolCall,
		Message: agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{{
			ID: "call-approval", Name: "delete_rule", Arguments: map[string]any{"rule_id": "rule-42"},
		}}},
	}
	responseWire := settlementEnvelope(t, &response)
	result, err := handler.HandleModelResponse(t.Context(), request.LoopID, response)
	require.NoError(t, err)
	require.Len(t, result.PublishedMessages, 1)
	callMsg, err := c.decoder.Decode(result.PublishedMessages[0].Data)
	require.NoError(t, err)
	call, ok := callMsg.Payload().(*agentic.ToolCall)
	require.True(t, ok)
	entity, err := handler.GetLoop(request.LoopID)
	require.NoError(t, err)
	c.loopsBucket = &approvalRevisionBucket{
		settlementBucket: &settlementBucket{values: map[string][]byte{entity.ID: settlementLoopRecord(t, entity)}},
		revisions:        make(map[string]uint64),
	}
	c.settlementEvidence = &settlementEvidence{
		request: retainedLoopMessage{subject: requestWire.Subject, data: requestWire.Data}, requestFound: true,
		response: retainedLoopMessage{subject: "agent.response." + request.RequestID, data: responseWire}, responseFound: true,
	}
	return c, &agentic.ToolResult{
		LoopID: call.LoopID, RequestID: call.RequestID, ExecutionID: call.ExecutionID,
		CallID: call.ID, CallOrdinal: call.CallOrdinal, Name: call.Name, TraceID: call.TraceID,
		ErrorKind: agentic.ToolErrorPermission, Error: agentic.ApprovalRequiredPrefix + "human approval required",
	}
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestApprovalGateConstructionFailureDoesNotReportSuccessfulWait(t *testing.T) {
	c, toolResult := approvalGateSettlementFixture(t)
	c.handler.config.Ports.Outputs = nil
	before, err := c.handler.GetLoop(toolResult.LoopID)
	require.NoError(t, err)

	result, err := c.handler.HandleToolResult(t.Context(), toolResult.LoopID, *toolResult)

	require.ErrorContains(t, err, `port name "agent.approval_pending" not found`)
	require.Equal(t, before.State, result.State, "failed gate construction must not report a successful approval wait")
	require.Empty(t, result.PublishedMessages)
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestApprovalGateRetainsRequiredResultAndSuccessfulWait(t *testing.T) {
	c, toolResult := approvalGateSettlementFixture(t)

	result, err := c.handler.HandleToolResult(t.Context(), toolResult.LoopID, *toolResult)

	require.NoError(t, err)
	require.Equal(t, agentic.LoopStateAwaitingApproval, result.State)
	require.Len(t, result.PublishedMessages, 1)
	baseMsg, err := c.decoder.Decode(result.PublishedMessages[0].Data)
	require.NoError(t, err)
	pending, ok := baseMsg.Payload().(*agentic.ApprovalPendingEvent)
	require.True(t, ok)
	require.Equal(t, toolResult.LoopID, pending.LoopID)
	require.Equal(t, toolResult.CallID, pending.CallID)

	entity, err := c.handler.GetLoop(toolResult.LoopID)
	require.NoError(t, err)
	require.NotNil(t, entity.PendingApproval)
	require.Equal(t, toolResult.ExecutionID, entity.PendingApproval.ExecutionID)
	require.Equal(t, *toolResult, entity.PendingToolResults[toolResult.ExecutionID],
		"awaiting approval must retain the result required before source ACK")

	// An already established wait still absorbs later results without advancing.
	result, err = c.handler.HandleToolResult(t.Context(), toolResult.LoopID, *toolResult)
	require.NoError(t, err)
	require.Equal(t, agentic.LoopStateAwaitingApproval, result.State)
	require.Empty(t, result.PublishedMessages)
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestApprovalGateFailureUsesExistingDeliveryClassification(t *testing.T) {
	for _, failure := range []string{"missing output"} {
		t.Run(failure, func(t *testing.T) {
			c, toolResult := approvalGateSettlementFixture(t)
			c.handler.config.Ports.Outputs = nil
			bucket := c.loopsBucket.(*approvalRevisionBucket)
			before := append([]byte(nil), bucket.values[toolResult.LoopID]...)
			msg := &loopDeliveryOwnerMsg{data: settlementEnvelope(t, toolResult)}

			result, admitted := consumeAdmittedDelivery(t.Context(), msg,
				task4HeartbeatPolicy(t, "tool.result", c.handleToolResultMessage), newDeliveryLaneAdmission(nil))

			require.True(t, admitted)
			require.Equal(t, natsclient.DeliveryDecisionTerminate, result.Decision())
			require.True(t, errs.IsInvalid(result.Err()), "existing invalid classification was lost: %v", result.Err())
			require.Zero(t, msg.acks.Load())
			require.Equal(t, int32(1), msg.terms.Load())
			require.Zero(t, msg.naks.Load())
			require.Equal(t, before, bucket.values[toolResult.LoopID], "failed setup must not persist successful approval state")
			_, err := c.handler.GetLoop(toolResult.LoopID)
			require.Error(t, err, "failed setup must discard speculative process state")
		})
	}
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestApprovalGateInvalidArgumentsUsesExistingOwnerClassification(t *testing.T) {
	c, toolResult := approvalGateSettlementFixture(t)
	args := c.handler.loopManager.GetToolArguments(toolResult.ExecutionID)
	args["invalid"] = make(chan struct{})
	c.handler.loopManager.TrackToolArguments(toolResult.ExecutionID, args)
	bucket := c.loopsBucket.(*approvalRevisionBucket)
	before := append([]byte(nil), bucket.values[toolResult.LoopID]...)
	// A Go channel cannot survive the retained JSON boundary. Exercise this
	// marshal fault at its direct handler owner and classifier, not as a
	// fabricated production delivery with supposedly valid retained bytes.
	result, err := c.handler.HandleToolResult(t.Context(), toolResult.LoopID, *toolResult)
	require.Error(t, err)
	require.True(t, errs.IsInvalid(err), "existing invalid classification was lost: %v", err)
	require.Equal(t, natsclient.DeliveryDecisionTerminate, loopSettlementDecision(err))
	require.Empty(t, result.PublishedMessages)
	require.NotEqual(t, agentic.LoopStateAwaitingApproval, result.State)
	require.Equal(t, before, bucket.values[toolResult.LoopID])
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestApprovalGateDurableFailureCannotAck(t *testing.T) {
	for _, failure := range []string{"loop state", "pending publication"} {
		t.Run(failure, func(t *testing.T) {
			c, toolResult := approvalGateSettlementFixture(t)
			if failure == "loop state" {
				c.loopsBucket = &approvalPutHookBucket{KeyValue: c.loopsBucket,
					beforePut: func(context.Context, string, []byte) error { return errors.New("approval state unavailable") }}
			} else {
				c.natsClient = &natsclient.Client{}
			}
			msg := &loopDeliveryOwnerMsg{data: settlementEnvelope(t, toolResult)}

			result, admitted := consumeAdmittedDelivery(t.Context(), msg,
				task4HeartbeatPolicy(t, "tool.result", c.handleToolResultMessage), newDeliveryLaneAdmission(nil))

			require.True(t, admitted)
			require.Equal(t, natsclient.DeliveryDecisionRetry, result.Decision())
			require.Error(t, result.Err())
			if failure == "loop state" {
				require.ErrorContains(t, result.Err(), "approval state unavailable")
			} else {
				require.ErrorContains(t, result.Err(), "publish result agent.approval_pending.")
			}
			require.Zero(t, msg.acks.Load()+msg.terms.Load())
			require.Equal(t, int32(1), msg.naks.Load())
			_, err := c.handler.GetLoop(toolResult.LoopID)
			require.Error(t, err, "failed durability must discard speculative process state")
		})
	}
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestApprovalGateAfterUnsettledCancellationCannotAck(t *testing.T) {
	c, toolResult := approvalGateSettlementFixture(t)
	c.natsClient = &natsclient.Client{}
	signal := &agentic.UserSignal{
		SignalID: "cancel-approval", Type: agentic.SignalCancel, LoopID: toolResult.LoopID,
		UserID: "user-approval", ChannelType: "test", ChannelID: "approval-channel",
	}
	cancelDecision, err := c.handleSignalMessage(t.Context(), settlementEnvelope(t, signal))
	require.ErrorContains(t, err, "cancellation completion has unknown durability")
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, cancelDecision)
	entity, err := c.handler.GetLoop(toolResult.LoopID)
	require.NoError(t, err)
	require.Equal(t, agentic.LoopStateCancelled, entity.State)
	bucket := c.loopsBucket.(*approvalRevisionBucket)
	before := append([]byte(nil), bucket.values[toolResult.LoopID]...)
	msg := &loopDeliveryOwnerMsg{data: settlementEnvelope(t, toolResult)}

	result, admitted := consumeAdmittedDelivery(t.Context(), msg,
		task4HeartbeatPolicy(t, "tool.result", c.handleToolResultMessage), newDeliveryLaneAdmission(nil))

	require.True(t, admitted)
	require.Zero(t, msg.acks.Load(), "terminal state alone does not prove the approval-required result applied")
	require.ErrorContains(t, result.Err(), "terminal loop lacks execution-specific retained result")
	require.Equal(t, before, bucket.values[toolResult.LoopID], "unsettled cancellation cannot authorize a new gate")
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestToolTimeoutWithoutConstructedFailureCannotAck(t *testing.T) {
	c, toolResult := approvalGateSettlementFixture(t)
	entity, err := c.handler.GetLoop(toolResult.LoopID)
	require.NoError(t, err)
	entity.TimeoutAt = time.Now().Add(-time.Second)
	require.NoError(t, c.handler.UpdateLoop(entity))
	bucket := c.loopsBucket.(*approvalRevisionBucket)
	_, err = bucket.Put(t.Context(), entity.ID, settlementLoopRecord(t, entity))
	require.NoError(t, err)
	before := append([]byte(nil), bucket.values[entity.ID]...)
	c.handler.config.Ports.Outputs = nil
	msg := &loopDeliveryOwnerMsg{data: settlementEnvelope(t, toolResult)}

	result, admitted := consumeAdmittedDelivery(t.Context(), msg,
		task4HeartbeatPolicy(t, "tool.result", c.handleToolResultMessage), newDeliveryLaneAdmission(nil))

	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision())
	require.ErrorContains(t, result.Err(), "loop timeout exceeded")
	require.Zero(t, msg.settlement.Load(), "failed terminal construction must not settle its source")
	require.Equal(t, before, bucket.values[entity.ID])
}
