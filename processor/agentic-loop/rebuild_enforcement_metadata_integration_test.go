//go:build integration

package agenticloop

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/stretchr/testify/require"
)

// enforcedTaskMetadata is the task-scoped ENFORCEMENT set of ADR-067: the
// read-only filesystem policy with its scratch exemptions, and the decide
// action allowlist. Every key is a member of DispatchEnforcedMetadataKeys, and
// the list is read from that variable rather than repeated, so a key added to
// the contract joins this fixture instead of quietly escaping it.
func enforcedTaskMetadata() map[string]any {
	metadata := map[string]any{
		agentic.MetadataKeyFilesystemPolicy:      agentic.FilesystemPolicyReadOnly,
		agentic.MetadataKeyScratchPaths:          []any{".semstreams/scratch"},
		agentic.MetadataKeyDecideActionAllowlist: []any{"approve", "reject"},
	}
	for _, key := range agentic.DispatchEnforcedMetadataKeys {
		if _, ok := metadata[key]; !ok {
			panic("enforcedTaskMetadata does not carry " + key +
				", so this test cannot see whether a rebuild restores it")
		}
	}
	return metadata
}

// toolExecuteSubjectFor resolves the subject a dispatched call goes out on
// through the SAME port resolution dispatchToolCall uses, so a port change
// moves the fixture and the production path together rather than leaving the
// test reading where nothing is published.
func toolExecuteSubjectFor(t *testing.T, toolName string) string {
	t.Helper()
	subject, err := component.ResolveSubject(DefaultConfig().Ports.Outputs, "tool.execute", toolName)
	require.NoError(t, err)
	return subject
}

// dispatchedToolCallOn reads the newest call the loop put on a subject,
// decoded through the envelope agentic-tools decodes. It is the EMITTED call,
// not the cache behind it: the executors read metadata off the call and off
// nothing else, so the cache is a mechanism and the call is the contract.
func dispatchedToolCallOn(t *testing.T, client *natsclient.Client, subject string) agentic.ToolCall {
	t.Helper()
	stream, err := client.GetStream(t.Context(), loopStreamName)
	require.NoError(t, err)
	stored, err := stream.GetLastMsgForSubject(t.Context(), subject)
	require.NoError(t, err, "nothing was dispatched on %s", subject)
	var envelope struct {
		Payload agentic.ToolCall `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(stored.Data, &envelope))
	return envelope.Payload
}

// requireEnforcedMetadata asserts the dispatched call carries every key
// dispatch stamps authoritatively, and that the policy is the RESTRICTIVE one
// the task set — an absent policy is not a neutral value on either consumer:
// agentic-tools' bash executor reads "" as the permissive default
// (FilesystemPolicyFromMetadata / IsReadOnlyPolicy) and decide permits any
// action when the allowlist is absent. Losing these keys turns a read-only
// task writable, silently.
func requireEnforcedMetadata(t *testing.T, call agentic.ToolCall, lane string) {
	t.Helper()
	require.Equal(t, agentic.FilesystemPolicyReadOnly, call.Metadata[agentic.MetadataKeyFilesystemPolicy],
		"%s: the rebuilt loop dispatched a read-only task's call with no filesystem policy; "+
			"the bash executor reads an absent policy as permissive", lane)
	require.Equal(t, []any{".semstreams/scratch"}, call.Metadata[agentic.MetadataKeyScratchPaths],
		"%s: the policy's scratch exemptions did not survive the rebuild", lane)
	require.Equal(t, []any{"approve", "reject"}, call.Metadata[agentic.MetadataKeyDecideActionAllowlist],
		"%s: the rebuilt loop dispatched with no decide allowlist; decide permits any action without one", lane)
}

// TestARebuiltLoopDispatchesWithItsTaskEnforcementMetadata is the enforcement
// half of the cold rebuild (owner Codex round on PR #1361, finding 1).
//
// dispatchToolCall stamps ADR-067's task-scoped enforcement keys onto every
// outgoing call from the loop's CACHED task metadata, and that cache is
// written once, at birth, by the process that handled the TaskMessage. A
// replacement never sees that task: it rebuilds the loop from the record and
// the retained request. The rebuild restored the request-side caches and not
// this one, so a recovered read-only task dispatched calls with no policy and
// no allowlist — and both consumers read an absent value as permissive.
//
// The record has carried the same metadata since birth, so there is a durable
// source in hand; what was missing was the restore. Both recovery entries are
// exercised, because they are separate code paths into the same dispatch: the
// cold response lane, and the cold tool lane whose apply releases a queued
// sibling.
//
// Assertions read the call off the stream rather than the cache, because the
// cache is the mechanism and the emitted call is what the executors enforce
// against.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestARebuiltLoopDispatchesWithItsTaskEnforcementMetadata(t *testing.T) {
	client := newLoopNATS(t)

	t.Run("the cold response lane", func(t *testing.T) {
		const loopID = "3a1c5e70-9d24-4b83-8f56-0e1d2c3b4a59"
		const toolName = "cold_response_probe"
		firstRequest := looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 0}.String()

		predecessor, _ := startLoopProcess(t, client, DefaultConfig())
		_, birth := deliverTask(t, predecessor, agentic.TaskMessage{
			TaskID:   "task-cold-response-enforcement",
			LoopID:   loopID,
			Role:     "general",
			Model:    "test-model",
			Prompt:   "a read-only task whose process is about to be replaced",
			Metadata: enforcedTaskMetadata(),
		})
		require.Equal(t, natsclient.DeliveryDecisionAck, birth.Decision())
		require.Equal(t, enforcedTaskMetadata(), loopRecordOf(t, predecessor, loopID).entity.Metadata,
			"the record is the rebuild's only durable source for these keys; without them on it "+
				"this arm would be testing a fixture rather than the recovery")

		// The replacement has no memory of the loop, so the model's answer
		// meets the cold response lane: rebuild from the record and the
		// retained request, then dispatch what the model asked for.
		replacement, replacementHandler := startLoopProcess(t, client, DefaultConfig())
		_, delivered := deliverResponse(t, replacement, agentic.AgentResponse{
			RequestID:    firstRequest,
			Status:       agentic.StatusToolCall,
			FinishReason: "tool_calls",
			Message: agentic.ChatMessage{
				Role:      "assistant",
				ToolCalls: []agentic.ToolCall{{ID: "call-cold-response", Name: toolName}},
			},
		})
		require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision())
		_, rebuilt := replacementHandler.loopManager.GetLoop(loopID)
		require.NoError(t, rebuilt, "the response was refused instead of rebuilding the loop it answers")

		requireEnforcedMetadata(t, dispatchedToolCallOn(t, client, toolExecuteSubjectFor(t, toolName)),
			"cold response")
	})

	t.Run("the cold tool lane releasing a queued sibling", func(t *testing.T) {
		const loopID = "6b2d8f31-4e57-4a90-9c13-5d6e7f809a2b"
		const appliedTool, arrivingTool, queuedTool = "batch_alpha", "batch_beta", "batch_gamma"
		firstRequest := looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 0}.String()

		predecessor, handler := startLoopProcess(t, client, DefaultConfig())
		_, birth := deliverTask(t, predecessor, agentic.TaskMessage{
			TaskID:   "task-cold-batch-enforcement",
			LoopID:   loopID,
			Role:     "general",
			Model:    "test-model",
			Prompt:   "a read-only task whose batch outlives its process",
			Metadata: enforcedTaskMetadata(),
		})
		require.Equal(t, natsclient.DeliveryDecisionAck, birth.Decision())

		// Three calls, so the batch survives two applies: one already on the
		// record, one arriving cold, one still queued when the replacement
		// takes over. The queued one is the call under test — the other two
		// were dispatched by the process that held the cache.
		batch := agentic.AgentResponse{
			RequestID:    firstRequest,
			Status:       agentic.StatusToolCall,
			FinishReason: "tool_calls",
			Message: agentic.ChatMessage{
				Role: "assistant",
				ToolCalls: []agentic.ToolCall{
					{ID: "call-batch-a", Name: appliedTool},
					{ID: "call-batch-b", Name: arrivingTool},
					{ID: "call-batch-c", Name: queuedTool},
				},
			},
		}
		retainModelResponse(t, client, batch)
		dispatch, err := handler.HandleModelResponse(t.Context(), loopID, batch)
		require.NoError(t, err)
		require.NoError(t, predecessor.persistHandlerResult(t.Context(), dispatch))
		callA, _ := dispatchedToolCall(t, dispatch)
		requireEnforcedMetadata(t, dispatchedToolCallOn(t, client, toolExecuteSubjectFor(t, appliedTool)),
			"warm dispatch (the premise: the holder of the cache stamps these keys)")

		_, appliedA := deliverToolResult(t, predecessor, agentic.ToolResult{
			CallID: callA.ID, Name: callA.Name, Content: "the first tool answered", LoopID: loopID,
			RequestID: callA.RequestID, ExecutionID: callA.ExecutionID, CallOrdinal: callA.CallOrdinal,
		})
		require.Equal(t, natsclient.DeliveryDecisionAck, appliedA.Decision())
		require.Equal(t, uint64(1), messagesOn(t, client, toolExecuteSubjectFor(t, arrivingTool)),
			"the applied result released the next call; the third is still queued")

		// The replacement rebuilds the batch from the record and the retained
		// response, applies the arriving result, and dispatches the sibling
		// that never ran — the first call this loop emits with no memory of
		// its task.
		replacement, _ := startLoopProcess(t, client, DefaultConfig())
		_, deliveredB := deliverToolResult(t, replacement, agentic.ToolResult{
			CallID: "call-batch-b", Name: arrivingTool, Content: "the second tool answered",
			LoopID: loopID, RequestID: firstRequest,
			ExecutionID: deriveToolExecutionID(firstRequest, "call-batch-b", 2), CallOrdinal: 2,
		})
		require.Equal(t, natsclient.DeliveryDecisionAck, deliveredB.Decision())
		require.Equal(t, uint64(1), messagesOn(t, client, toolExecuteSubjectFor(t, queuedTool)),
			"the rebuilt loop must go on running the batch it recovered")

		requireEnforcedMetadata(t, dispatchedToolCallOn(t, client, toolExecuteSubjectFor(t, queuedTool)),
			"cold tool batch")
	})
}
