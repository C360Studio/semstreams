//go:build integration

package agenticloop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type approvalReplacementExecutor struct{ calls atomic.Int32 }

func (e *approvalReplacementExecutor) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	e.calls.Add(1)
	return agentic.ToolResult{CallID: call.ID, Name: call.Name, Content: "approved rule-42"}, nil
}

func (*approvalReplacementExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{Name: "approval_probe", Parameters: map[string]any{"type": "object"}}}
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
// First gate only: approve after the original ToolResult and dispatch events have
// settled. Real task/model/tools owners produce every retained checkpoint; no
// process cache or durable loop record is seeded. Graph/evidence E2E, the other
// decisions, and approval-response redelivery remain separate proofs.
func TestIntegrationApprovalAfterLoopAndDispatchReplacement(t *testing.T) {
	// This test owns the bounded root so controlled owner cleanup precedes
	// parent cancellation; testing cancels t.Context before Cleanup callbacks.
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	const suffix = "approval-replacement"
	const callID = "provider-approval-call"
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>", "tool.>"}},
		natsclient.TestStreamConfig{Name: "USER", Subjects: []string{"user.>"}},
	))
	stream, err := tc.Client.GetStream(ctx, "AGENT")
	require.NoError(t, err)
	decoder := payloadbuiltins.NewTestDecoder(t)
	toolCalls, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name: "approval-redispatch-observer", FilterSubject: "tool.execute.>", AckPolicy: jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)
	var providerCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		var request struct {
			Messages []agentic.ChatMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		answer := map[string]any{"role": "assistant", "tool_calls": []map[string]any{{
			"id": callID, "type": "function", "function": map[string]any{
				"name": "approval_probe", "arguments": `{"rule_id":"rule-42"}`,
			},
		}}}
		finish := "tool_calls"
		for _, msg := range request.Messages {
			if msg.Role == "tool" && msg.Content == "approved rule-42" {
				answer = map[string]any{"role": "assistant", "content": "approval completed"}
				finish = "stop"
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": answer, "finish_reason": finish}},
			"usage":   map[string]int{"prompt_tokens": 40, "completion_tokens": 10},
		}); err != nil {
			t.Errorf("encode deterministic provider response: %v", err)
		}
	}))
	t.Cleanup(provider.Close)
	deps := component.Dependencies{
		NATSClient: tc.Client, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
		ModelRegistry: &model.Registry{
			Endpoints: map[string]*model.EndpointConfig{
				"approval-test": {URL: provider.URL + "/v1/chat/completions", Model: "approval-test", MaxTokens: 128000},
			},
			Defaults: model.DefaultsConfig{Model: "approval-test"},
		},
	}
	marshal := func(value any) []byte {
		t.Helper()
		data, err := json.Marshal(value)
		require.NoError(t, err)
		return data
	}
	modelConfig := agenticmodel.DefaultConfig()
	modelConfig.ConsumerNameSuffix, modelConfig.Timeout = suffix, "5s"
	modelOwner, err := agenticmodel.NewComponent(marshal(modelConfig), deps)
	require.NoError(t, err)
	toolsConfig := agentictools.DefaultConfig()
	toolsConfig.ConsumerNameSuffix = suffix
	toolsConfig.ApprovalRequired = []string{"approval_probe"}
	toolsOwner, err := agentictools.NewComponent(marshal(toolsConfig), deps)
	require.NoError(t, err)
	executor := &approvalReplacementExecutor{}
	require.NoError(t, toolsOwner.(*agentictools.Component).RegisterToolExecutor(executor))
	for _, discoverable := range []component.Discoverable{modelOwner, toolsOwner} {
		owner := discoverable.(component.LifecycleComponent)
		require.NoError(t, owner.Initialize())
		require.NoError(t, owner.Start(ctx))
		t.Cleanup(func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer stopCancel()
			require.NoError(t, owner.Stop(stopCtx))
		})
	}
	settled := map[string]chan *terminalMarkerDelivery{
		"tool.result":             make(chan *terminalMarkerDelivery, 4),
		"agent.approval_response": make(chan *terminalMarkerDelivery, 4),
	}
	startOwners := func() (*http.ServeMux, func()) {
		t.Helper()
		loopConfig := DefaultConfig()
		loopConfig.ConsumerNameSuffix, loopConfig.MaxIterations = suffix, 3
		loopConfig.Timeout = "60s"
		loop, err := NewComponent(marshal(loopConfig), deps)
		require.NoError(t, err)
		c := loop.(*Component)
		// Same scope as the existing started-owner proofs: graph evidence is
		// excluded; settlement-required KV writes and publications stay real.
		c.graphWriter = nil
		var handles []jetstream.ConsumeContext
		c.consumeStream = func(setupCtx, ownerCtx context.Context, port natsclient.PortConsumerContext,
			cfg natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg),
		) (jetstream.ConsumeContext, error) {
			handle, err := tc.Client.ConsumeStreamWithConfigContexts(setupCtx, ownerCtx, port, cfg,
				func(msgCtx context.Context, msg jetstream.Msg) {
					if port.Port != "tool.result" && port.Port != "agent.approval_response" {
						handler(msgCtx, msg)
						return
					}
					observed := &terminalMarkerDelivery{Msg: msg}
					if port.Port == "tool.result" {
						observed.beforeAck = func() error {
							base, err := decoder.Decode(msg.Data())
							if err != nil {
								return err
							}
							result, ok := base.Payload().(*agentic.ToolResult)
							if !ok || !strings.HasPrefix(result.Error, agentic.ApprovalRequiredPrefix) {
								return nil
							}
							entity, found, err := c.readLoopEntity(msgCtx, result.LoopID)
							if err != nil || !found || entity.State != agentic.LoopStateAwaitingApproval ||
								entity.PendingApproval == nil || !reflect.DeepEqual(entity.PendingToolResults[result.ExecutionID], *result) {
								return fmt.Errorf("approval source ACK preceded exact pending persistence: found=%v err=%v", found, err)
							}
							_, err = stream.GetLastMsgForSubject(msgCtx, "agent.approval_pending."+result.LoopID)
							return err
						}
					}
					handler(msgCtx, observed)
					// Native methods delegate unchanged. Handoff after callback return
					// synchronizes the existing wrapper's settlement counters.
					settled[port.Port] <- observed
				})
			if err == nil {
				handles = append(handles, handle)
			}
			return handle, err
		}
		dispatchConfig := agenticdispatch.DefaultConfig()
		dispatchConfig.ConsumerNameSuffix = suffix
		dispatch, err := agenticdispatch.NewComponent(marshal(dispatchConfig), deps)
		require.NoError(t, err)
		runCtx, runCancel := context.WithCancel(ctx)
		var started []component.LifecycleComponent
		stop := func() {
			t.Helper()
			// Controlled Stop retains its accepted parent until every owner joins.
			// Parent cancellation before Stop would instead exercise abort policy.
			defer runCancel()
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer stopCancel()
			for i := len(started) - 1; i >= 0; i-- {
				require.NoError(t, started[i].Stop(stopCtx))
			}
			started = nil
			for _, handle := range handles {
				select {
				case <-handle.Closed():
				default:
					t.Error("loop Stop returned before exact consumer Closed")
				}
			}
		}
		t.Cleanup(stop)
		for _, discoverable := range []component.Discoverable{loop, dispatch} {
			owner := discoverable.(component.LifecycleComponent)
			require.NoError(t, owner.Initialize())
			require.NoError(t, owner.Start(runCtx))
			started = append(started, owner)
		}
		require.Empty(t, c.handler.loopManager.loops, "Start must not receive another instance's process state")
		mux := http.NewServeMux()
		dispatch.(*agenticdispatch.Component).RegisterHTTPHandlers("/", mux)
		return mux, stop
	}
	post := func(mux *http.ServeMux, path string, body any, user string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(marshal(body)))
		req = req.WithContext(agenticdispatch.WithIdentity(ctx, user))
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, req)
		return recorder
	}
	wait := func(port string) *terminalMarkerDelivery {
		t.Helper()
		select {
		case observed := <-settled[port]:
			return observed
		case <-time.After(5 * time.Second):
			t.Fatalf("started %s delivery owner did not return", port)
			return nil
		}
	}
	eventFloors := make(map[string]uint64)
	waitSettled := func() {
		t.Helper()
		require.EventuallyWithT(t, func(collect *assert.CollectT) {
			seen := make(map[string]bool)
			lister := stream.ListConsumers(ctx)
			for info := range lister.Info() {
				if strings.HasSuffix(info.Name, "-"+suffix) {
					seen[info.Config.FilterSubject] = true
					require.Zero(collect, info.NumAckPending, "%s", info.Name)
					require.Zero(collect, info.NumPending, "%s", info.Name)
					if sequence, ok := eventFloors[info.Config.FilterSubject]; ok {
						require.GreaterOrEqual(collect, info.AckFloor.Stream, sequence, "%s", info.Name)
					}
				}
			}
			require.NoError(collect, lister.Err())
			// These ACKed events must not repopulate the replacement tracker.
			require.True(collect, seen["agent.created.*"])
			require.True(collect, seen["agent.approval_pending.*"])
		}, 5*time.Second, 10*time.Millisecond)
	}
	firstMux, stopFirst := startOwners()
	submitted := post(firstMux, "/message", agenticdispatch.HTTPMessageRequest{
		Content: "Run approval_probe on rule-42", ChannelType: "http", ChannelID: "approval-session",
	}, "task-owner")
	require.Equal(t, http.StatusOK, submitted.Code, "%s", submitted.Body.String())
	var submission agenticdispatch.HTTPMessageResponse
	require.NoError(t, json.Unmarshal(submitted.Body.Bytes(), &submission))
	loopID := submission.InReplyTo
	require.NotEmpty(t, loopID)
	initial := wait("tool.result")
	require.Equal(t, 1, initial.acks)
	require.Zero(t, initial.naks+initial.terms)
	require.NoError(t, initial.ackCheckErr)
	base, err := decoder.Decode(initial.Data())
	require.NoError(t, err)
	gated, ok := base.Payload().(*agentic.ToolResult)
	require.True(t, ok)
	require.Equal(t, agentic.ToolErrorPermission, gated.ErrorKind)
	require.True(t, strings.HasPrefix(gated.Error, agentic.ApprovalRequiredPrefix))
	require.Equal(t, loopID, gated.LoopID)
	require.Zero(t, executor.calls.Load(), "approval gate must precede the executor")
	require.Equal(t, int32(1), providerCalls.Load())
	originalMsg, err := toolCalls.Next(jetstream.FetchMaxWait(5 * time.Second))
	require.NoError(t, err)
	base, err = decoder.Decode(originalMsg.Data())
	require.NoError(t, err)
	original, ok := base.Payload().(*agentic.ToolCall)
	require.True(t, ok)
	require.NoError(t, original.Validate())
	require.Empty(t, original.ApprovedBy)
	require.NoError(t, originalMsg.DoubleAck(ctx))
	bucket, err := tc.Client.GetKeyValueBucket(ctx, "AGENT_LOOPS")
	require.NoError(t, err)
	entry, err := bucket.Get(ctx, loopID)
	require.NoError(t, err)
	var pending agentic.LoopEntity
	require.NoError(t, json.Unmarshal(entry.Value(), &pending))
	require.NoError(t, pending.Validate())
	require.Equal(t, agentic.LoopStateAwaitingApproval, pending.State)
	require.NotNil(t, pending.PendingApproval)
	require.Equal(t, *gated, pending.PendingToolResults[pending.PendingApproval.ExecutionID])
	require.Equal(t, original.RequestID, pending.PendingApproval.RequestID)
	require.Equal(t, original.ExecutionID, pending.PendingApproval.ExecutionID)
	require.Equal(t, original.CallOrdinal, pending.PendingApproval.CallOrdinal)
	require.Equal(t, original.ID, pending.PendingApproval.CallID)
	require.Equal(t, original.Name, pending.PendingApproval.ToolName)
	require.Equal(t, original.Arguments, pending.PendingApproval.Arguments)
	// Typed trace is optional on this production HTTP/model path. Preserve
	// exactly what it produced (including empty), not a fixture-invented trace.
	require.Equal(t, original.TraceID, pending.PendingApproval.TraceID)
	require.Equal(t, original.TraceID, gated.TraceID)
	requestRaw, err := stream.GetLastMsgForSubject(ctx, "agent.request."+loopID)
	require.NoError(t, err)
	base, err = decoder.Decode(requestRaw.Data)
	require.NoError(t, err)
	request, ok := base.Payload().(*agentic.AgentRequest)
	require.True(t, ok)
	require.Equal(t, original.RequestID, request.RequestID)
	responseRaw, err := stream.GetLastMsgForSubject(ctx, "agent.response."+request.RequestID)
	require.NoError(t, err)
	base, err = decoder.Decode(responseRaw.Data)
	require.NoError(t, err)
	response, ok := base.Payload().(*agentic.AgentResponse)
	require.True(t, ok)
	require.Len(t, response.Message.ToolCalls, 1)
	require.Equal(t, original.ID, response.Message.ToolCalls[0].ID)
	require.Equal(t, original.Arguments, response.Message.ToolCalls[0].Arguments)
	require.Equal(t, original.TraceID, response.Message.ToolCalls[0].TraceID)
	for _, prefix := range []string{"agent.created.", "agent.approval_pending."} {
		event, err := stream.GetLastMsgForSubject(ctx, prefix+loopID)
		require.NoError(t, err)
		eventFloors[prefix+"*"] = event.Sequence
	}
	waitSettled()
	metadata, err := initial.Metadata()
	require.NoError(t, err)
	consumer, err := stream.Consumer(ctx, metadata.Consumer)
	require.NoError(t, err)
	info, err := consumer.Info(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, info.AckFloor.Stream, metadata.Sequence.Stream)
	t.Logf("fully settled approval checkpoint: loop=%s request=%s execution=%s tool_result_seq=%d ack_floor=%d kv_revision=%d", loopID, request.RequestID, gated.ExecutionID, metadata.Sequence.Stream, info.AckFloor.Stream, entry.Revision())
	stopFirst()
	firstMux = nil
	replacementMux, stopReplacement := startOwners()
	unchanged, err := bucket.Get(ctx, loopID)
	require.NoError(t, err)
	require.Equal(t, entry.Value(), unchanged.Value(), "replacement must use retained pending authority")
	approved := post(replacementMux, "/loops/"+loopID+"/approval", agenticdispatch.ApprovalRequest{
		Decision: agentic.ApprovalDecisionApprove,
	}, "second-party-reviewer")
	require.Equal(t, http.StatusOK, approved.Code, "replacement HTTP approval must read retained pending state: %s", approved.Body.String())
	approval := wait("agent.approval_response")
	require.Equal(t, 1, approval.acks)
	require.Zero(t, approval.naks+approval.terms)
	base, err = decoder.Decode(approval.Data())
	require.NoError(t, err)
	decision, ok := base.Payload().(*agentic.ApprovalResponse)
	require.True(t, ok)
	require.Equal(t, loopID, decision.LoopID)
	require.Equal(t, original.ID, decision.CallID)
	redispatched, err := toolCalls.Next(jetstream.FetchMaxWait(5 * time.Second))
	require.NoError(t, err, "fresh loop must resume the exact approved call")
	base, err = decoder.Decode(redispatched.Data())
	require.NoError(t, err)
	call, ok := base.Payload().(*agentic.ToolCall)
	require.True(t, ok)
	want := *original
	want.ApprovedBy = "second-party-reviewer"
	require.Equal(t, want, *call, "approval must preserve request/execution/ordinal/call/arguments/trace")
	require.NoError(t, redispatched.DoubleAck(ctx))
	terminal := wait("tool.result")
	require.Equal(t, 1, terminal.acks)
	require.Zero(t, terminal.naks+terminal.terms)
	base, err = decoder.Decode(terminal.Data())
	require.NoError(t, err)
	result, ok := base.Payload().(*agentic.ToolResult)
	require.True(t, ok)
	require.Equal(t, loopID, result.LoopID)
	require.Equal(t, original.RequestID, result.RequestID)
	require.Equal(t, original.ExecutionID, result.ExecutionID)
	require.Equal(t, original.CallOrdinal, result.CallOrdinal)
	require.Equal(t, original.ID, result.CallID)
	require.Equal(t, original.Name, result.Name)
	require.Equal(t, original.TraceID, result.TraceID)
	require.Equal(t, "approved rule-42", result.Content)
	// ToolResult ACK proves the next request's publication, not its eventual
	// response. Await the exact final marker before inspecting consumer snapshots
	// or stopping owners; ListConsumers is not an atomic cross-consumer barrier.
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		finalEntry, err := bucket.Get(ctx, loopID)
		require.NoError(collect, err)
		var final agentic.LoopEntity
		require.NoError(collect, json.Unmarshal(finalEntry.Value(), &final))
		require.Equal(collect, pending.ID, final.ID)
		require.Equal(collect, pending.TaskID, final.TaskID)
		require.Equal(collect, agentic.LoopStateComplete, final.State)
		require.Equal(collect, agentic.OutcomeSuccess, final.Outcome)
		require.Equal(collect, "approval completed", final.Result)
		require.Nil(collect, final.PendingApproval)
	}, 10*time.Second, 10*time.Millisecond)
	waitSettled()
	require.Equal(t, int32(1), executor.calls.Load())
	require.Equal(t, int32(2), providerCalls.Load())
	stopReplacement()
}
