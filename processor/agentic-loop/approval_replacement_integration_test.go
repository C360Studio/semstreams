//go:build integration

package agenticloop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type approvalReplacementExecutor struct{ calls atomic.Int32 }

// This wrapper interrupts only source ACK. Every other method, including
// metadata and immediate NAK, delegates to the actual server delivery.
type approvalReplacementDelivery struct {
	*terminalMarkerDelivery
	interruptAck bool
}

func (m *approvalReplacementDelivery) Ack() error {
	if m.interruptAck {
		m.acks++
		if m.beforeAck != nil {
			m.ackCheckErr = m.beforeAck()
			if m.ackCheckErr != nil {
				return m.ackCheckErr
			}
		}
		return errors.New("test replacement interrupted source ACK after application")
	}
	return m.terminalMarkerDelivery.Ack()
}

func (m *approvalReplacementDelivery) Nak() error {
	m.naks++
	return m.Msg.Nak()
}

func (e *approvalReplacementExecutor) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	e.calls.Add(1)
	return agentic.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("approved %v", call.Arguments["rule_id"])}, nil
}

func (*approvalReplacementExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{Name: "approval_probe", Parameters: map[string]any{"type": "object"}},
		{Name: "prior_probe", Parameters: map[string]any{"type": "object"}},
	}
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
func TestIntegrationApprovalAfterLoopAndDispatchReplacement(t *testing.T) {
	testApprovalAfterReplacement(t, agenticdispatch.ApprovalRequest{Decision: agentic.ApprovalDecisionApprove}, false, false, "")
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
// spec: agentic-loop / Per-loop in-process state is released at terminal, through the one release point
func TestIntegrationAppliedApprovalRedeliversAfterOwnerReplacement(t *testing.T) {
	testApprovalAfterReplacement(t, agenticdispatch.ApprovalRequest{Decision: agentic.ApprovalDecisionApprove}, false, false, "agent.approval_response")
}

// spec: agentic-loop / Approval-required tool statuses settle by observed execution phase
func TestIntegrationApprovalRequiredResultRedeliversAfterClosedGate(t *testing.T) {
	testApprovalAfterReplacement(t, agenticdispatch.ApprovalRequest{Decision: agentic.ApprovalDecisionApprove}, false, false, "tool.result")
}

// spec: agentic-loop / Approval-required tool statuses settle by observed execution phase
func TestIntegrationApprovalRequiredResultRedeliversAfterLaterHistory(t *testing.T) {
	testApprovalAfterReplacement(t, agenticdispatch.ApprovalRequest{
		Decision: agentic.ApprovalDecisionReject, Reason: "retain rule-42 for audit",
	}, false, false, "tool.result")
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
func TestIntegrationModifiedApprovalAfterLoopAndDispatchReplacement(t *testing.T) {
	testApprovalAfterReplacement(t, agenticdispatch.ApprovalRequest{
		Decision: agentic.ApprovalDecisionModify, ModifiedArguments: map[string]any{"rule_id": "rule-99"},
	}, false, false, "")
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
func TestIntegrationRejectedApprovalAfterLoopAndDispatchReplacement(t *testing.T) {
	testApprovalAfterReplacement(t, agenticdispatch.ApprovalRequest{
		Decision: agentic.ApprovalDecisionReject, Reason: "retain rule-42 for audit",
	}, false, false, "")
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
func TestIntegrationApprovalReplacementIgnoresOlderSameCallIDResponse(t *testing.T) {
	testApprovalAfterReplacement(t, agenticdispatch.ApprovalRequest{Decision: agentic.ApprovalDecisionApprove}, true, false, "")
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
// spec: agentic-loop / Approval deadlines are reconstructed narrowly
func TestIntegrationApprovalTimeoutAfterLoopAndDispatchReplacement(t *testing.T) {
	testApprovalAfterReplacement(t, agenticdispatch.ApprovalRequest{
		Decision: agentic.ApprovalDecisionReject, Reason: "approval timed out after 8s",
	}, false, true, "")
}

// Real task/model/tools owners produce every retained checkpoint; no process
// cache or durable loop record is seeded. Graph/evidence E2E and
// OS-process replacement remain separate proofs.
func testApprovalAfterReplacement(t *testing.T, approvalRequest agenticdispatch.ApprovalRequest, retainOlderResponse, timeout bool, redeliverPort string) {
	t.Helper()
	laterHistory := redeliverPort == "tool.result" && approvalRequest.Decision == agentic.ApprovalDecisionReject
	const laterFailureContent = "Tool error: tool call had empty function name — call a specific tool by name or respond with text"
	approver := "second-party-reviewer"
	if timeout {
		approver = approvalTimeoutSystemApprover
	}
	wantToolContent := "approved rule-42"
	wantExecutions := int32(1)
	wantProviderCalls := int32(2)
	if laterHistory {
		wantProviderCalls++
	}
	priorExecutions := int32(0)
	if retainOlderResponse {
		priorExecutions = 1
		wantExecutions++
		wantProviderCalls++
	}
	if approvalRequest.Decision == agentic.ApprovalDecisionModify {
		require.NotNil(t, approvalRequest.ModifiedArguments)
		require.NotEqual(t, map[string]any{"rule_id": "rule-42"}, approvalRequest.ModifiedArguments)
		wantToolContent = "approved rule-99"
	} else if approvalRequest.Decision == agentic.ApprovalDecisionReject {
		wantToolContent = "Tool error: " + agentic.ApprovalRejectedPrefix + "rejected by " + approver + ": " + approvalRequest.Reason
		wantExecutions = 0
	}
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
		Durable: "approval-redispatch-observer",
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
		toolName, arguments := "approval_probe", `{"rule_id":"rule-42"}`
		if retainOlderResponse {
			toolName, arguments = "prior_probe", `{"rule_id":"rule-old"}`
			for _, msg := range request.Messages {
				if msg.Role == "tool" && msg.Content == "approved rule-old" {
					toolName, arguments = "approval_probe", `{"rule_id":"rule-42"}`
				}
			}
		}
		answer := map[string]any{"role": "assistant", "tool_calls": []map[string]any{{
			"id": callID, "type": "function", "function": map[string]any{
				"name": toolName, "arguments": arguments,
			},
		}}}
		finish := "tool_calls"
		for _, msg := range request.Messages {
			if msg.Role == "tool" && msg.Content == wantToolContent {
				answer = map[string]any{"role": "assistant", "content": "approval completed"}
				finish = "stop"
				if laterHistory {
					// The original tool.result holds MaxAckPending=1. An existing
					// inline malformed-provider-call recovery supplies the newer
					// batch result without another external tool.result delivery.
					answer = map[string]any{"role": "assistant", "tool_calls": []map[string]any{{
						"id": callID, "type": "function", "function": map[string]any{"name": "", "arguments": `{}`},
					}}}
					finish = "tool_calls"
				}
			}
			if laterHistory && msg.Role == "tool" && msg.Content == laterFailureContent {
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
	settled := map[string]chan *approvalReplacementDelivery{
		"tool.result":             make(chan *approvalReplacementDelivery, 4),
		"agent.approval_response": make(chan *approvalReplacementDelivery, 4),
	}
	var replayLogs bytes.Buffer // Read/reset only after the owning component has joined.
	var closedGateBefore jetstream.KeyValueEntry
	var closedPromptSequence uint64
	var supersededBefore float64
	var settledStreamSequence uint64
	startOwners := func(approvalTimeout, interruptAckPort string) (*http.ServeMux, func()) {
		t.Helper()
		loopConfig := DefaultConfig()
		loopConfig.ConsumerNameSuffix, loopConfig.MaxIterations = suffix, 3
		loopConfig.Timeout = "60s"
		loopConfig.ApprovalTimeoutStr = approvalTimeout
		loop, err := NewComponent(marshal(loopConfig), deps)
		require.NoError(t, err)
		c := loop.(*Component)
		if redeliverPort == "tool.result" {
			c.logger = slog.New(slog.NewTextHandler(&replayLogs, nil))
		}
		require.Empty(t, c.handler.loopManager.loops, "replacement must not be seeded with another instance's process state")
		// Same scope as the existing started-owner proofs: graph evidence is
		// excluded; settlement-required KV writes and publications stay real.
		c.graphWriter = nil
		var handles []jetstream.ConsumeContext
		var sourceObserved atomic.Bool
		c.consumeStream = func(setupCtx, ownerCtx context.Context, port natsclient.PortConsumerContext,
			cfg natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg),
		) (jetstream.ConsumeContext, error) {
			handle, err := tc.Client.ConsumeStreamWithConfigContexts(setupCtx, ownerCtx, port, cfg,
				func(msgCtx context.Context, msg jetstream.Msg) {
					if port.Port != "tool.result" && port.Port != "agent.approval_response" {
						handler(msgCtx, msg)
						return
					}
					gatedResult := false
					if port.Port == "tool.result" {
						if base, err := decoder.Decode(msg.Data()); err == nil {
							if result, ok := base.Payload().(*agentic.ToolResult); ok {
								gatedResult = agentic.IsApprovalRequired(result.Error)
							}
						}
					}
					selectedSource := port.Port == redeliverPort && (port.Port != "tool.result" || gatedResult)
					observed := &approvalReplacementDelivery{
						terminalMarkerDelivery: &terminalMarkerDelivery{Msg: msg},
						interruptAck:           port.Port == interruptAckPort && selectedSource,
					}
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
							metadata, err := msg.Metadata()
							if err != nil {
								return err
							}
							// The first source ACK requires the persisted gate and prompt.
							// Later superseded delivery correctly observes a closed gate.
							if metadata.NumDelivered > 1 {
								if redeliverPort == "tool.result" {
									// The original source still occupies MaxAckPending=1.
									// Check closure before its real ACK releases the final result.
									if closedGateBefore == nil {
										return errors.New("old gated source redelivered before independent closure was captured")
									}
									current, err := c.loopsBucket.Get(msgCtx, result.LoopID)
									if err != nil {
										return err
									}
									if current.Revision() != closedGateBefore.Revision() || !bytes.Equal(current.Value(), closedGateBefore.Value()) {
										return errors.New("superseded status changed closed gate authority before ACK")
									}
									wantProviderBeforeAck := wantProviderCalls - 1
									if laterHistory {
										wantProviderBeforeAck = wantProviderCalls
										streamInfo, err := stream.Info(msgCtx)
										if err != nil {
											return err
										}
										if streamInfo.State.LastSeq != settledStreamSequence || len(perLoopMapCount(c.handler.loopManager, result.LoopID)) != 0 {
											return errors.New("historical supersession published output or restored process state before ACK")
										}
									}
									if executor.calls.Load() != wantExecutions || providerCalls.Load() != wantProviderBeforeAck {
										return errors.New("superseded status repeated an executor or model effect before ACK")
									}
									if testutil.ToFloat64(getMetrics(nil).approvalStatusesSuperseded) != supersededBefore+1 {
										return errors.New("superseded status did not emit its diagnostic counter before ACK")
									}
									prompt, err := stream.GetLastMsgForSubject(msgCtx, "agent.approval_pending."+result.LoopID)
									if err != nil {
										return err
									}
									if prompt.Sequence != closedPromptSequence {
										return errors.New("superseded status published another pending prompt before ACK")
									}
								}
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
					// Handoff after callback return synchronizes settlement counters.
					// Only the selected original source ACK may be interrupted.
					if selectedSource && !sourceObserved.CompareAndSwap(false, true) {
						// Observe the first attempt from each owner. Later immediate
						// retries still run the native callback without blocking Stop.
						return
					}
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
		if !timeout {
			require.Empty(t, c.handler.loopManager.loops, "Start must not receive another instance's process state")
		}
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
	wait := func(port string) *approvalReplacementDelivery {
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
	waitSettled := func(withheldFilter string) {
		t.Helper()
		require.EventuallyWithT(t, func(collect *assert.CollectT) {
			seen := make(map[string]bool)
			lister := stream.ListConsumers(ctx)
			for info := range lister.Info() {
				if strings.HasSuffix(info.Name, "-"+suffix) {
					seen[info.Config.FilterSubject] = true
					if withheldFilter != "" && info.Config.FilterSubject == withheldFilter {
						require.Equal(collect, 1, info.NumAckPending, "%s", info.Name)
					} else {
						require.Zero(collect, info.NumAckPending, "%s", info.Name)
					}
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
			require.True(collect, seen["agent.approval_response.*"])
			if withheldFilter != "" {
				require.True(collect, seen[withheldFilter])
			}
		}, 5*time.Second, 10*time.Millisecond)
	}
	initialTimeout, replacementTimeout := "", ""
	if timeout {
		initialTimeout, replacementTimeout = "8s", "1m"
	}
	firstInterruptedPort := ""
	if redeliverPort == "tool.result" {
		firstInterruptedPort = redeliverPort
	}
	firstMux, stopFirst := startOwners(initialTimeout, firstInterruptedPort)
	submitted := post(firstMux, "/message", agenticdispatch.HTTPMessageRequest{
		Content: "Run approval_probe on rule-42", ChannelType: "http", ChannelID: "approval-session",
	}, "task-owner")
	require.Equal(t, http.StatusOK, submitted.Code, "%s", submitted.Body.String())
	var submission agenticdispatch.HTTPMessageResponse
	require.NoError(t, json.Unmarshal(submitted.Body.Bytes(), &submission))
	loopID := submission.InReplyTo
	require.NotEmpty(t, loopID)
	var olderResponse *agentic.AgentResponse
	if retainOlderResponse {
		prior := wait("tool.result")
		require.Equal(t, 1, prior.acks)
		require.Zero(t, prior.naks+prior.terms)
		base, err := decoder.Decode(prior.Data())
		require.NoError(t, err)
		result, ok := base.Payload().(*agentic.ToolResult)
		require.True(t, ok)
		require.Empty(t, result.Error)
		require.Equal(t, "approved rule-old", result.Content)
		priorCallMsg, err := toolCalls.Next(jetstream.FetchMaxWait(5 * time.Second))
		require.NoError(t, err)
		require.NoError(t, priorCallMsg.DoubleAck(ctx))
		olderRaw, err := stream.GetLastMsgForSubject(ctx, "agent.response."+result.RequestID)
		require.NoError(t, err)
		base, err = decoder.Decode(olderRaw.Data)
		require.NoError(t, err)
		olderResponse, ok = base.Payload().(*agentic.AgentResponse)
		require.True(t, ok)
		require.Len(t, olderResponse.Message.ToolCalls, 1)
		require.Equal(t, result.CallID, olderResponse.Message.ToolCalls[0].ID)
		require.Equal(t, map[string]any{"rule_id": "rule-old"}, olderResponse.Message.ToolCalls[0].Arguments)
	}
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
	require.Equal(t, priorExecutions, executor.calls.Load(), "approval gate must precede the gated executor")
	require.Equal(t, 1+priorExecutions, providerCalls.Load())
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
	if retainOlderResponse {
		require.NotEqual(t, olderResponse.RequestID, request.RequestID)
		require.Equal(t, olderResponse.Message.ToolCalls[0].ID, original.ID)
		require.NotEqual(t, olderResponse.Message.ToolCalls[0].Arguments, original.Arguments)
		// Both exact subjects remain present; this is not a replaced single
		// response. The latest request above names only the current response.
		retainedOld, err := stream.GetLastMsgForSubject(ctx, "agent.response."+olderResponse.RequestID)
		require.NoError(t, err)
		require.Less(t, retainedOld.Sequence, responseRaw.Sequence)
		t.Logf("retained same-CallID responses: older_request=%s older_seq=%d current_request=%s current_seq=%d call=%s",
			olderResponse.RequestID, retainedOld.Sequence, request.RequestID, responseRaw.Sequence, original.ID)
	}
	for _, prefix := range []string{"agent.created.", "agent.approval_pending."} {
		event, err := stream.GetLastMsgForSubject(ctx, prefix+loopID)
		require.NoError(t, err)
		if prefix == "agent.approval_pending." {
			base, err := decoder.Decode(event.Data)
			require.NoError(t, err)
			prompt, ok := base.Payload().(*agentic.ApprovalPendingEvent)
			require.True(t, ok)
			require.Equal(t, original.ExecutionID, prompt.ExecutionID)
			// Retain the exact identity shown before replacement; do not ask
			// the replacement owner to select a gate on behalf of this prompt.
			approvalRequest.ExecutionID = prompt.ExecutionID
		}
		eventFloors[prefix+"*"] = event.Sequence
	}
	metadata, err := initial.Metadata()
	require.NoError(t, err)
	consumer, err := stream.Consumer(ctx, metadata.Consumer)
	require.NoError(t, err)
	info, err := consumer.Info(ctx)
	require.NoError(t, err)
	withheldFilter := ""
	if redeliverPort == "tool.result" {
		withheldFilter = info.Config.FilterSubject
		require.Equal(t, 30*time.Second, info.Config.AckWait, "keep the production durable's native redelivery interval")
		require.Equal(t, uint64(1), metadata.NumDelivered)
	}
	waitSettled(withheldFilter)
	info, err = consumer.Info(ctx)
	require.NoError(t, err)
	if redeliverPort == "tool.result" {
		require.Less(t, info.AckFloor.Stream, metadata.Sequence.Stream)
	} else {
		require.GreaterOrEqual(t, info.AckFloor.Stream, metadata.Sequence.Stream)
	}
	t.Logf("approval checkpoint: loop=%s request=%s execution=%s tool_result_seq=%d ack_floor=%d kv_revision=%d withheld_filter=%q source=%s", loopID, request.RequestID, gated.ExecutionID, metadata.Sequence.Stream, info.AckFloor.Stream, entry.Revision(), withheldFilter, initial.Data())
	stopFirst()
	firstMux = nil
	if timeout {
		require.Equal(t, 8*time.Second, pending.PendingApproval.Timeout)
		require.True(t, time.Now().Before(pending.PendingApproval.RequestedAt.Add(pending.PendingApproval.Timeout)),
			"first owner must stop before the original deadline; an expired warm owner would not prove replacement")
		stoppedRequest, err := stream.GetLastMsgForSubject(ctx, "agent.request."+loopID)
		require.NoError(t, err)
		require.Equal(t, requestRaw.Sequence, stoppedRequest.Sequence, "first owner must not have published a timeout continuation")
		t.Logf("retained approval deadline: requested_at=%s timeout=%s deadline=%s replacement_config=%s",
			pending.PendingApproval.RequestedAt.Format(time.RFC3339Nano), pending.PendingApproval.Timeout,
			pending.PendingApproval.RequestedAt.Add(pending.PendingApproval.Timeout).Format(time.RFC3339Nano), replacementTimeout)
	}
	replacementInterruptedPort := ""
	if redeliverPort == "agent.approval_response" {
		replacementInterruptedPort = redeliverPort
	}
	replacementMux, stopReplacement := startOwners(replacementTimeout, replacementInterruptedPort)
	var appliedApproval *approvalReplacementDelivery
	unchanged, err := bucket.Get(ctx, loopID)
	require.NoError(t, err)
	require.Equal(t, entry.Value(), unchanged.Value(), "replacement must use retained pending authority")
	if timeout {
		// Exercise the real five-second ticker without an HTTP decision or a
		// manual handler call. A fifteen-second window allows three cadence
		// intervals and the original eight-second deadline, not a new minute.
		continued := assert.EventuallyWithT(t, func(collect *assert.CollectT) {
			raw, err := stream.GetLastMsgForSubject(ctx, "agent.request."+loopID)
			require.NoError(collect, err)
			base, err := decoder.Decode(raw.Data)
			require.NoError(collect, err)
			request, ok := base.Payload().(*agentic.AgentRequest)
			require.True(collect, ok)
			require.NotEqual(collect, original.RequestID, request.RequestID,
				"replacement sweeper must publish the timeout continuation using retained pending authority")
		}, 3*approvalSweepInterval, 20*time.Millisecond)
		if !continued {
			current, readErr := bucket.Get(ctx, loopID)
			if readErr == nil {
				t.Logf("timeout RED retained state: revision=%d unchanged=%v bytes=%s", current.Revision(),
					bytes.Equal(entry.Value(), current.Value()), current.Value())
			}
			mirror, mirrorErr := stream.GetLastMsgForSubject(ctx, "agent.approval_response."+loopID)
			t.Logf("timeout mirror observation: message=%v read_error=%v callbacks=%d", mirror, mirrorErr,
				len(settled["agent.approval_response"]))
			t.Fatalf("no timeout continuation: authority_read=%v executor_calls=%d provider_calls=%d",
				readErr, executor.calls.Load(), providerCalls.Load())
		}
		require.False(t, time.Now().Before(pending.PendingApproval.RequestedAt.Add(pending.PendingApproval.Timeout)),
			"timeout must not run before the original persisted deadline")
		approval := wait("agent.approval_response")
		appliedApproval = approval
		require.Equal(t, 1, approval.acks, "the native timeout input owns application and settlement")
		require.Zero(t, approval.naks+approval.terms)
		base, err = decoder.Decode(approval.Data())
		require.NoError(t, err)
		decision, ok := base.Payload().(*agentic.ApprovalResponse)
		require.True(t, ok)
		require.Equal(t, loopID, decision.LoopID)
		require.Equal(t, original.ID, decision.CallID)
		require.Equal(t, original.ExecutionID, decision.ExecutionID)
		require.Equal(t, agentic.ApprovalDecisionReject, decision.Decision)
		require.Equal(t, approvalRequest.Reason, decision.Reason)
		require.Equal(t, approvalTimeoutSystemApprover, decision.ApprovedBy)
		require.False(t, decision.DecidedAt.Before(pending.PendingApproval.RequestedAt.Add(pending.PendingApproval.Timeout)))
		metadata, err := approval.Metadata()
		require.NoError(t, err)
		eventFloors["agent.approval_response.*"] = metadata.Sequence.Stream
		t.Logf("native timeout source settled: sequence=%d ack=%d nak=%d term=%d", metadata.Sequence.Stream,
			approval.acks, approval.naks, approval.terms)
	} else {
		approved := post(replacementMux, "/loops/"+loopID+"/approval", approvalRequest, approver)
		require.Equal(t, http.StatusOK, approved.Code, "replacement HTTP approval must read retained pending state: %s", approved.Body.String())
		approval := wait("agent.approval_response")
		appliedApproval = approval
		require.Equal(t, 1, approval.acks)
		require.Zero(t, approval.naks+approval.terms)
		base, err = decoder.Decode(approval.Data())
		require.NoError(t, err)
		decision, ok := base.Payload().(*agentic.ApprovalResponse)
		require.True(t, ok)
		require.Equal(t, loopID, decision.LoopID)
		require.Equal(t, original.ID, decision.CallID)
		require.Equal(t, approvalRequest.ExecutionID, decision.ExecutionID)
		require.Equal(t, approvalRequest.Decision, decision.Decision)
		require.Equal(t, approvalRequest.ModifiedArguments, decision.ModifiedArguments)
		require.Equal(t, approvalRequest.Reason, decision.Reason)
		require.Equal(t, approver, decision.ApprovedBy)
	}
	if approvalRequest.Decision != agentic.ApprovalDecisionReject {
		redispatched, err := toolCalls.Next(jetstream.FetchMaxWait(5 * time.Second))
		require.NoError(t, err, "fresh loop must resume the exact approved call")
		base, err = decoder.Decode(redispatched.Data())
		require.NoError(t, err)
		call, ok := base.Payload().(*agentic.ToolCall)
		require.True(t, ok)
		want := *original
		want.ApprovedBy = "second-party-reviewer"
		if approvalRequest.Decision == agentic.ApprovalDecisionModify {
			want.Arguments = approvalRequest.ModifiedArguments
		}
		require.Equal(t, want, *call, "approval must preserve request/execution/ordinal/call/arguments/trace")
		require.NoError(t, redispatched.DoubleAck(ctx))
		if redeliverPort == "tool.result" {
			// ApprovalResponse is an independent consumer and commits closure.
			// The original unacked result holds the sole tool.result slot, so
			// the completed approved result must remain queued until replay ACK.
			require.EventuallyWithT(t, func(collect *assert.CollectT) {
				info, err := consumer.Info(ctx)
				require.NoError(collect, err)
				require.Equal(collect, 1, info.Config.MaxAckPending)
				require.Equal(collect, 1, info.NumAckPending)
				require.Equal(collect, uint64(1), info.NumPending)
			}, 5*time.Second, 10*time.Millisecond)
			closedEntry, err := bucket.Get(ctx, loopID)
			require.NoError(t, err)
			var closed agentic.LoopEntity
			require.NoError(t, json.Unmarshal(closedEntry.Value(), &closed))
			require.Equal(t, pending.StateBeforeApproval, closed.State)
			require.Nil(t, closed.PendingApproval)
			require.Equal(t, *gated, closed.PendingToolResults[gated.ExecutionID])
			require.Equal(t, wantExecutions, executor.calls.Load())
			require.Equal(t, wantProviderCalls-1, providerCalls.Load())
			t.Logf("closed gate before old source replay: source_seq=%d source=%s kv_revision=%d state=%s pending=%s request=%s response=%s",
				metadata.Sequence.Stream, initial.Data(), closedEntry.Revision(), closedEntry.Value(),
				entry.Value(), requestRaw.Data, responseRaw.Data)
			stopReplacement()
			replacementMux = nil
			// Publish the read-only witness to callbacks only after the previous
			// owner joins and before the next owner starts.
			closedGateBefore = closedEntry
			closedPromptSequence = eventFloors["agent.approval_pending.*"]
			replayLogs.Reset()
			supersededBefore = testutil.ToFloat64(getMetrics(nil).approvalStatusesSuperseded)
			_, stopReplacement = startOwners(replacementTimeout, "")
			var replay *approvalReplacementDelivery
			select {
			case replay = <-settled["tool.result"]:
			case <-time.After(info.Config.AckWait + 5*time.Second):
				t.Fatal("original gated ToolResult did not redeliver to the third started owner")
			}
			replayedMeta, err := replay.Metadata()
			require.NoError(t, err)
			require.Equal(t, metadata.Stream, replayedMeta.Stream)
			require.Equal(t, metadata.Consumer, replayedMeta.Consumer)
			require.Equal(t, metadata.Sequence.Stream, replayedMeta.Sequence.Stream)
			require.Equal(t, uint64(2), replayedMeta.NumDelivered)
			require.Equal(t, initial.Subject(), replay.Subject())
			require.Equal(t, initial.Data(), replay.Data())
			t.Logf("old gated ToolResult replay: sequence=%d delivered=%d ack=%d nak=%d term=%d pre_ack_error=%v",
				replayedMeta.Sequence.Stream, replayedMeta.NumDelivered, replay.acks, replay.naks, replay.terms, replay.ackCheckErr)
			require.NoError(t, replay.ackCheckErr)
			require.Equal(t, 1, replay.acks)
			require.Zero(t, replay.naks+replay.terms)
			eventFloors[info.Config.FilterSubject] = replayedMeta.Sequence.Stream
		}
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
		require.Equal(t, wantToolContent, result.Content)
	}
	// Rejection is a synthetic permission result, not another tool.result
	// publication. Inspect the production request's paired conversation directly.
	if laterHistory {
		require.EventuallyWithT(t, func(collect *assert.CollectT) {
			current, err := bucket.Get(ctx, loopID)
			require.NoError(collect, err)
			var entity agentic.LoopEntity
			require.NoError(collect, json.Unmarshal(current.Value(), &entity))
			require.Equal(collect, agentic.LoopStateComplete, entity.State)
			_, oldResultRetained := entity.PendingToolResults[original.ExecutionID]
			require.False(collect, oldResultRetained, "normal newer batch must replace the old accumulator before replay")
		}, 5*time.Second, 10*time.Millisecond)
	}
	nextRaw, err := stream.GetLastMsgForSubject(ctx, "agent.request."+loopID)
	require.NoError(t, err)
	base, err = decoder.Decode(nextRaw.Data)
	require.NoError(t, err)
	next, ok := base.Payload().(*agentic.AgentRequest)
	require.True(t, ok)
	require.NotEqual(t, original.RequestID, next.RequestID)
	var toolMessages []agentic.ChatMessage
	for i, msg := range next.Messages {
		if msg.Role != "tool" {
			continue
		}
		toolMessages = append(toolMessages, msg)
		require.Positive(t, i)
		require.Equal(t, "assistant", next.Messages[i-1].Role)
		require.Len(t, next.Messages[i-1].ToolCalls, 1)
		require.Equal(t, original.ID, next.Messages[i-1].ToolCalls[0].ID)
		if retainOlderResponse && msg.Name == "prior_probe" {
			require.Equal(t, olderResponse.Message.ToolCalls[0].Arguments, next.Messages[i-1].ToolCalls[0].Arguments)
		} else if laterHistory && msg.Name == "invalid_tool_call" {
			laterCall := next.Messages[i-1].ToolCalls[0]
			require.Empty(t, laterCall.Name)
			require.NotEqual(t, original.RequestID, laterCall.RequestID)
			require.NotEqual(t, original.ExecutionID, laterCall.ExecutionID)
			require.Equal(t, uint32(1), laterCall.CallOrdinal)
		} else {
			require.Equal(t, original.Arguments, next.Messages[i-1].ToolCalls[0].Arguments)
			if laterHistory {
				call := next.Messages[i-1].ToolCalls[0]
				require.Equal(t, original.RequestID, call.RequestID)
				require.Equal(t, original.ExecutionID, call.ExecutionID)
				require.Equal(t, original.CallOrdinal, call.CallOrdinal)
				require.Equal(t, original.Name, call.Name)
				require.Equal(t, original.TraceID, call.TraceID)
			}
		}
	}
	wantToolMessages := []agentic.ChatMessage{{Role: "tool", ToolCallID: original.ID,
		Name: original.Name, Content: wantToolContent, IsError: approvalRequest.Decision == agentic.ApprovalDecisionReject,
	}}
	if retainOlderResponse {
		wantToolMessages = append([]agentic.ChatMessage{{Role: "tool", ToolCallID: original.ID,
			Name: "prior_probe", Content: "approved rule-old",
		}}, wantToolMessages...)
	}
	if laterHistory {
		wantToolMessages = append(wantToolMessages, agentic.ChatMessage{Role: "tool", ToolCallID: original.ID,
			Name: "invalid_tool_call", Content: laterFailureContent, IsError: true})
	}
	require.Equal(t, wantToolMessages, toolMessages)
	// Source ACK proves the next request's publication, not its eventual response.
	// Await the exact final marker before inspecting consumer snapshots or stopping
	// owners; ListConsumers is not an atomic cross-consumer barrier.
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
	if redeliverPort == "agent.approval_response" || laterHistory {
		// The real branch has completed; the only unsettled input must be the
		// original source whose ACK was interrupted. Nothing fabricates redelivery.
		appliedSource := appliedApproval
		if laterHistory {
			appliedSource = initial
		}
		firstMeta, err := appliedSource.Metadata()
		require.NoError(t, err)
		require.Equal(t, uint64(1), firstMeta.NumDelivered)
		sourceConsumer, err := stream.Consumer(ctx, firstMeta.Consumer)
		require.NoError(t, err)
		before, err := sourceConsumer.Info(ctx)
		require.NoError(t, err)
		waitSettled(before.Config.FilterSubject)
		before, err = sourceConsumer.Info(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, before.NumAckPending)
		require.Zero(t, before.NumPending)
		require.Less(t, before.AckFloor.Stream, firstMeta.Sequence.Stream)
		finalBefore, err := bucket.Get(ctx, loopID)
		require.NoError(t, err)
		t.Logf("applied %s before replacement: source=%d delivered=%d ack_pending=%d kv_revision=%d next_request=%s request_seq=%d executions=%d provider_calls=%d state=%s",
			redeliverPort, firstMeta.Sequence.Stream, firstMeta.NumDelivered, before.NumAckPending, finalBefore.Revision(),
			next.RequestID, nextRaw.Sequence, executor.calls.Load(), providerCalls.Load(), finalBefore.Value())
		t.Logf("retained approval inputs: pending=%s request=%s response=%s source=%s",
			entry.Value(), requestRaw.Data, responseRaw.Data, appliedSource.Data())
		t.Logf("retained applied next request: %s", nextRaw.Data)
		stopReplacement()
		replacementMux = nil
		skipCounter := getMetrics(nil).approvalDecisionsInapplicable
		if laterHistory {
			var final agentic.LoopEntity
			require.NoError(t, json.Unmarshal(finalBefore.Value(), &final))
			_, retained := final.PendingToolResults[original.ExecutionID]
			require.False(t, retained, "old execution must rely on exact history, not the current accumulator")
			closedGateBefore = finalBefore
			closedPromptSequence = eventFloors["agent.approval_pending.*"]
			streamInfo, err := stream.Info(ctx)
			require.NoError(t, err)
			settledStreamSequence = streamInfo.State.LastSeq
			replayLogs.Reset()
			supersededBefore = testutil.ToFloat64(getMetrics(nil).approvalStatusesSuperseded)
			skipCounter = getMetrics(nil).approvalStatusesSuperseded
			t.Logf("native later-history boundary: original_seq=%d latest_request_seq=%d stream_seq=%d old_execution=%s current_results=%v",
				firstMeta.Sequence.Stream, nextRaw.Sequence, settledStreamSequence, original.ExecutionID, final.PendingToolResults)
		}
		skipsBefore := testutil.ToFloat64(skipCounter)
		_, stopReplay := startOwners(replacementTimeout, "")
		var replay *approvalReplacementDelivery
		select {
		case replay = <-settled[redeliverPort]:
		case <-time.After(before.Config.AckWait + 5*time.Second):
			t.Fatalf("unacknowledged %s did not redeliver to the started replacement", redeliverPort)
		}
		stopReplay()
		replayedMeta, err := replay.Metadata()
		require.NoError(t, err)
		require.Equal(t, firstMeta.Stream, replayedMeta.Stream)
		require.Equal(t, firstMeta.Consumer, replayedMeta.Consumer)
		require.Equal(t, firstMeta.Sequence.Stream, replayedMeta.Sequence.Stream)
		require.Greater(t, replayedMeta.NumDelivered, firstMeta.NumDelivered)
		require.Equal(t, appliedSource.Subject(), replay.Subject())
		require.Equal(t, appliedSource.Data(), replay.Data())
		finalAfter, err := bucket.Get(ctx, loopID)
		require.NoError(t, err)
		require.Equal(t, finalBefore.Revision(), finalAfter.Revision())
		require.Equal(t, finalBefore.Value(), finalAfter.Value())
		require.Equal(t, wantExecutions, executor.calls.Load(), "approval replay must not repeat the gated executor effect")
		require.Equal(t, wantProviderCalls, providerCalls.Load())
		t.Logf("applied %s replay: source=%d delivered=%d ack=%d nak=%d term=%d executions=%d provider_calls=%d",
			redeliverPort, replayedMeta.Sequence.Stream, replayedMeta.NumDelivered, replay.acks, replay.naks, replay.terms,
			executor.calls.Load(), providerCalls.Load())
		require.Equal(t, 1, replay.acks, "the exact replay must settle from its current-authority proof")
		require.Zero(t, replay.naks+replay.terms)
		require.NoError(t, replay.ackCheckErr)
		require.Equal(t, skipsBefore+1, testutil.ToFloat64(skipCounter))
		eventFloors[before.Config.FilterSubject] = replayedMeta.Sequence.Stream
	}
	waitSettled("")
	require.Equal(t, wantExecutions, executor.calls.Load())
	require.Equal(t, wantProviderCalls, providerCalls.Load())
	stopReplacement()
	if redeliverPort == "tool.result" {
		require.Contains(t, replayLogs.String(), "approval-required tool status superseded by observed execution phase")
		require.Contains(t, replayLogs.String(), "loop_id="+loopID)
		require.Contains(t, replayLogs.String(), "execution_id="+gated.ExecutionID)
		require.Equal(t, supersededBefore+1, testutil.ToFloat64(getMetrics(nil).approvalStatusesSuperseded))
		t.Logf("supersession diagnostic: %s", replayLogs.String())
	}
	observerInfo, err := toolCalls.Info(ctx)
	require.NoError(t, err)
	require.Zero(t, observerInfo.NumPending, "no unobserved tool redispatch after completion")
	require.Zero(t, observerInfo.NumAckPending)
	require.Empty(t, settled["tool.result"], "reject must not publish a synthetic tool.result; other branches settle exactly one result")
}
