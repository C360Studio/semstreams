//go:build integration

package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func newRetainedVerdictProcess(t *testing.T, client *natsclient.Client, suffix string) *Component {
	t.Helper()
	cfg := DefaultConfig()
	cfg.ConsumerNameSuffix = suffix
	cfg.ToolCallGovernance = ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "5s"}
	for i := range cfg.Ports.Inputs {
		port := &cfg.Ports.Inputs[i]
		if port.Name == "agent.toolcall.approved" || port.Name == "agent.toolcall.rejected" {
			port.Config = component.JetStreamPort{Subjects: []string{port.Name + ".>"}, StreamName: "VERDICT_" + port.Name[len("agent.toolcall."):]}
		}
	}
	for i := range cfg.Ports.Outputs {
		port := &cfg.Ports.Outputs[i]
		if port.Name == "agent.toolcall.proposed" {
			port.Config = component.JetStreamPort{Subjects: []string{port.Name + ".*"}, StreamName: "PROPOSALS"}
		}
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	discoverable, err := NewComponent(data, component.Dependencies{NATSClient: client, PayloadRegistry: payloadbuiltins.NewTestRegistry(t)})
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.graphWriter = nil // Graph observations are not settlement-required evidence.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, c.Stop(ctx))
	})
	return c
}

type retainedVerdictReplacementCase struct {
	name, retained, current      string
	dual, publishFailure, cancel bool
}

// spec: agentic-governance / Governance publications are durably at-least-once
// spec: agentic-governance / Governance verdicts use one registered wire and typed handoff
// spec: agentic-governance / Governance verdict correlation survives process replacement
// Seeded verdicts and the controlled current-policy publisher below prove the
// loop response boundary, not #1311's separate proposal-source settlement owner.
func TestIntegrationResponseRetainedVerdictReuseAfterReplacement(t *testing.T) {
	runResponseVerdictReplacementCases(t, []retainedVerdictReplacementCase{
		{name: "retained-approved", retained: "approved"},
		{name: "retained-rejected", retained: "rejected"},
	})
}

// spec: agentic-governance / Governance publications are durably at-least-once
// spec: agentic-governance / Governance verdicts use one registered wire and typed handoff
// spec: agentic-governance / Governance verdict correlation survives process replacement
func TestIntegrationResponseAbsentVerdictCurrentPolicyAfterReplacement(t *testing.T) {
	runResponseVerdictReplacementCases(t, []retainedVerdictReplacementCase{
		{name: "absent-current-approved", current: "approved"},
		{name: "absent-current-rejected", current: "rejected"},
	})
}

// spec: agentic-governance / Governance publications are durably at-least-once
// spec: agentic-governance / Governance verdicts use one registered wire and typed handoff
// spec: agentic-governance / Governance verdict correlation survives process replacement
func TestIntegrationResponseGovernanceRefusalAfterReplacement(t *testing.T) {
	runResponseVerdictReplacementCases(t, []retainedVerdictReplacementCase{
		{name: "dual-match", retained: "approved", dual: true},
		{name: "required-publication-failure", publishFailure: true},
		{name: "operation-cancellation", cancel: true},
	})
}

func runResponseVerdictReplacementCases(t *testing.T, rows []retainedVerdictReplacementCase) {
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			ctx := t.Context()
			tc := natsclient.NewTestClient(t, natsclient.WithStreams(
				natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.task.>", "agent.request.>", "agent.response.>", "agent.created.>", "agent.complete.>", "agent.failed.>", "agent.signal.>", "agent.approval_response.>", "agent.approval_pending.>", "agent.context.>", "tool.>"}},
				natsclient.TestStreamConfig{Name: "PROPOSALS", Subjects: []string{"agent.toolcall.proposed.>"}},
				natsclient.TestStreamConfig{Name: "VERDICT_approved", Subjects: []string{"agent.toolcall.approved.>"}},
				natsclient.TestStreamConfig{Name: "VERDICT_rejected", Subjects: []string{"agent.toolcall.rejected.>"}},
			))
			first := newRetainedVerdictProcess(t, tc.Client, row.name)
			first.handler.SetGovernanceDispatcher(retainedVerdictPolicy{propose: func(context.Context, string, string, []agentic.ToolCall) (DispatcherResult, error) {
				return DispatcherResult{}, errors.New("controlled replacement before current-policy evaluation")
			}})
			firstCtx, cancelFirst := context.WithCancel(ctx)
			defer cancelFirst()
			taskSettled := make(chan struct{}, 1)
			firstSettled := make(chan *approvalReplacementDelivery, 1)
			first.consumeStream = func(setupCtx, ownerCtx context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
				return tc.Client.ConsumeStreamWithConfigContexts(setupCtx, ownerCtx, owner, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
					if firstCtx.Err() != nil {
						return
					}
					if owner.Port == "agent.response" {
						observed := &approvalReplacementDelivery{terminalMarkerDelivery: &terminalMarkerDelivery{Msg: msg}}
						handler(msgCtx, observed)
						cancelFirst()
						firstSettled <- observed
						return
					}
					handler(msgCtx, msg)
					if owner.Port == "agent.task" {
						taskSettled <- struct{}{}
					}
				})
			}
			require.NoError(t, first.Start(firstCtx))
			// Native adapter: only an exact missing message in an existing stream
			// is absence. A missing stream and canceled I/O must remain failures.
			reader := natsLoopSettlementEvidenceReader{client: tc.Client}
			_, found, err := reader.ReadGovernanceVerdict(ctx, "VERDICT_approved", "agent.toolcall.approved.absent")
			require.NoError(t, err)
			require.False(t, found)
			_, found, err = reader.ReadGovernanceVerdict(ctx, "MISSING_VERDICT_STREAM", "agent.toolcall.approved.absent")
			require.Error(t, err)
			require.False(t, found)
			canceled, cancelRead := context.WithCancel(ctx)
			cancelRead()
			_, _, err = reader.ReadGovernanceVerdict(canceled, "VERDICT_approved", "agent.toolcall.approved.absent")
			require.Error(t, err)
			loopID := uuid.NewString()
			task := &agentic.TaskMessage{LoopID: loopID, TaskID: "retained-verdict-task", Role: "general", Model: "test-model", Prompt: "look up the result"}
			require.NoError(t, tc.Client.PublishToStream(ctx, "agent.task.test", settlementEnvelope(t, task)))
			select {
			case <-taskSettled:
			case <-time.After(5 * time.Second):
				t.Fatal("task intake did not settle")
			}
			stream, err := tc.Client.GetStream(ctx, "AGENT")
			require.NoError(t, err)
			requestRaw, err := stream.GetLastMsgForSubject(ctx, "agent.request."+loopID)
			require.NoError(t, err)
			decoded, err := first.decoder.Decode(requestRaw.Data)
			require.NoError(t, err)
			request, ok := decoded.Payload().(*agentic.AgentRequest)
			require.True(t, ok)
			before, err := first.loopsBucket.Get(ctx, loopID)
			require.NoError(t, err)
			calls := []agentic.ToolCall{{ID: "provider-call", Name: "lookup", Arguments: map[string]any{"key": "value"}}}
			response := &agentic.AgentResponse{RequestID: request.RequestID, Status: agentic.StatusToolCall, Message: agentic.ChatMessage{Role: "assistant", ToolCalls: append([]agentic.ToolCall(nil), calls...)}}
			responseData := settlementEnvelope(t, response)
			require.NoError(t, stampToolExecutionCorrelation(request.RequestID, calls))
			proposal, err := prepareProposedToolCall(loopID, "", calls[0])
			require.NoError(t, err)
			require.NoError(t, tc.Client.PublishToStream(ctx, "agent.response."+request.RequestID, responseData))
			var failed *approvalReplacementDelivery
			select {
			case failed = <-firstSettled:
			case <-time.After(5 * time.Second):
				t.Fatal("first response did not reach its owner")
			}
			require.Equal(t, 1, failed.naks)
			require.Zero(t, failed.acks+failed.terms)
			firstMetadata, err := failed.Metadata()
			require.NoError(t, err)
			require.NoError(t, first.Stop(ctx)) // Joins all original callbacks before replacement.
			consumer, err := stream.Consumer(ctx, firstMetadata.Consumer)
			require.NoError(t, err)
			info, err := consumer.Info(ctx)
			require.NoError(t, err)
			require.Equal(t, 1, info.NumAckPending)
			for _, decision := range []string{"approved", "rejected"} {
				if decision == row.retained || row.dual {
					verdict := verdictForPublishedProposal(proposal, decision)
					verdict.Reason = "native retained policy decision"
					require.NoError(t, tc.Client.PublishToStream(ctx, "agent.toolcall."+decision+"."+calls[0].ExecutionID, registeredProposalVerdict(t, verdict, false)))
				}
			}
			proposals, err := tc.Client.GetStream(ctx, "PROPOSALS")
			require.NoError(t, err)
			if row.publishFailure {
				info, infoErr := proposals.Info(ctx)
				require.NoError(t, infoErr)
				config := info.Config
				config.Discard, config.MaxMsgs = jetstream.DiscardNew, 1
				js, jsErr := tc.Client.JetStream()
				require.NoError(t, jsErr)
				_, err = js.UpdateStream(ctx, config)
				require.NoError(t, err)
				require.NoError(t, tc.Client.PublishToStream(ctx, "agent.toolcall.proposed.capacity", []byte("occupied capacity")))
			}
			replacement := newRetainedVerdictProcess(t, tc.Client, row.name)
			replacementCtx, cancelReplacement := context.WithCancel(ctx)
			defer cancelReplacement()
			var publications atomic.Int32
			// Capture inside the existing owner timeout, not the outer delivery
			// callback: retained reads and publication must share this operation.
			var responseOpCtx context.Context
			replacement.settlementEvidence = &approvalClosureEvidence{
				loopSettlementEvidenceReader: replacement.settlementEvidence,
				afterRequest:                 func(opCtx context.Context) { responseOpCtx = opCtx },
			}
			publisher := proposalTestPublisher(func(opCtx context.Context, subject string, data []byte) error {
				if opCtx != responseOpCtx {
					return errors.New("publication replaced the response operation context")
				}
				publications.Add(1)
				if row.cancel {
					cancelReplacement()
					<-opCtx.Done()
					return opCtx.Err()
				}
				if err := tc.Client.PublishToStream(opCtx, subject, data); err != nil {
					return err
				}
				if row.current != "" {
					actual := publishedGovernanceProposal(t, data)
					verdict := verdictForPublishedProposal(actual, row.current)
					verdict.Reason = "native current policy decision"
					return tc.Client.PublishToStream(opCtx, "agent.toolcall."+row.current+"."+actual.ExecutionID, registeredProposalVerdict(t, verdict, true))
				}
				return errors.New("retained verdict unexpectedly re-proposed")
			})
			replacement.handler.SetGovernanceDispatcher(NewGovernanceDispatcher(replacement.config.ToolCallGovernance, publisher, replacement.logger, nil))
			settled := make(chan *approvalReplacementDelivery, 1)
			handles := make(map[string]*fastLaneObservedConsumer)
			replacement.consumeStream = func(setupCtx, ownerCtx context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
				handle, consumeErr := tc.Client.ConsumeStreamWithConfigContexts(setupCtx, ownerCtx, owner, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
					if owner.Port != "agent.response" {
						handler(msgCtx, msg)
						return
					}
					if replacementCtx.Err() != nil {
						return
					}
					observed := &approvalReplacementDelivery{terminalMarkerDelivery: &terminalMarkerDelivery{Msg: msg, beforeAck: func() error {
						decision := row.retained
						if decision == "" {
							decision = row.current
						}
						if decision == "approved" {
							raw, readErr := stream.GetLastMsgForSubject(msgCtx, "tool.execute.lookup")
							if readErr != nil {
								return readErr
							}
							decoded, decodeErr := replacement.decoder.Decode(raw.Data)
							if decodeErr != nil {
								return decodeErr
							}
							call, ok := decoded.Payload().(*agentic.ToolCall)
							if !ok || call.ExecutionID != calls[0].ExecutionID || call.RequestID != request.RequestID || call.CallOrdinal != 1 {
								return fmt.Errorf("source ACK preceded correlated tool work: %+v", decoded.Payload())
							}
						} else {
							raw, readErr := stream.GetLastMsgForSubject(msgCtx, "agent.request."+loopID)
							if readErr != nil {
								return readErr
							}
							if raw.Sequence <= requestRaw.Sequence {
								return errors.New("source ACK preceded rejection follow-up request")
							}
							decoded, decodeErr := replacement.decoder.Decode(raw.Data)
							if decodeErr != nil {
								return decodeErr
							}
							next, ok := decoded.Payload().(*agentic.AgentRequest)
							if !ok || next.RequestID == request.RequestID {
								return errors.New("rejection did not produce a registered next request")
							}
							wantReason := "native retained policy decision"
							if row.current != "" {
								wantReason = "native current policy decision"
							}
							foundRejection := false
							for _, entry := range next.Messages {
								if entry.Role == "tool" && entry.ToolCallID == calls[0].ID && entry.IsError && strings.Contains(entry.Content, wantReason) {
									foundRejection = true
								}
							}
							if !foundRejection {
								return fmt.Errorf("next request lacks the exact policy rejection: %+v", next.Messages)
							}
						}
						return nil
					}}}
					handler(msgCtx, observed)
					if row.publishFailure {
						cancelReplacement()
					}
					settled <- observed
				})
				if consumeErr != nil {
					return nil, consumeErr
				}
				observed := &fastLaneObservedConsumer{ConsumeContext: handle}
				handles[owner.Port] = observed
				return observed, nil
			}
			require.NoError(t, replacement.Start(replacementCtx))
			var redelivered *approvalReplacementDelivery
			select {
			case redelivered = <-settled:
			case <-time.After(40 * time.Second):
				t.Fatal("replacement did not receive the retained source (production Retry delay is unchanged)")
			}
			metadata, err := redelivered.Metadata()
			require.NoError(t, err)
			require.Equal(t, firstMetadata.Sequence.Stream, metadata.Sequence.Stream)
			require.Equal(t, firstMetadata.Consumer, metadata.Consumer)
			require.Greater(t, metadata.NumDelivered, firstMetadata.NumDelivered)
			require.Equal(t, responseData, redelivered.Data())
			if row.dual || row.publishFailure || row.cancel {
				require.Zero(t, redelivered.acks+redelivered.terms)
				if row.dual {
					require.Zero(t, redelivered.naks)
					require.Eventually(t, func() bool { return handles["agent.response"].drains.Load() == 1 }, 5*time.Second, 10*time.Millisecond)
					for port, handle := range handles {
						if port != "agent.response" {
							require.Zero(t, handle.drains.Load(), "unrelated owner %s stopped", port)
						}
					}
					replacement.mu.RLock()
					fatalErr := replacement.deliveryFatalErr
					replacement.mu.RUnlock()
					require.Error(t, fatalErr)
				} else {
					require.Equal(t, 1, redelivered.naks)
				}
				after, readErr := replacement.loopsBucket.Get(ctx, loopID)
				require.NoError(t, readErr)
				require.Equal(t, before.Value(), after.Value())
				require.Equal(t, before.Revision(), after.Revision())
				_, readErr = replacement.loopsBucket.Get(ctx, "COMPLETE_"+loopID)
				require.ErrorIs(t, readErr, jetstream.ErrKeyNotFound)
				require.Empty(t, perLoopMapCount(replacement.handler.loopManager, loopID))
			} else {
				require.NoError(t, redelivered.ackCheckErr)
				require.Equal(t, 1, redelivered.acks)
				require.Zero(t, redelivered.naks+redelivered.terms)
			}
			wantPublications := int32(0)
			if row.current != "" || row.publishFailure || row.cancel {
				wantPublications = 1
			}
			require.Equal(t, wantPublications, publications.Load())
			if row.retained != "approved" && row.current != "approved" || row.dual {
				_, readErr := stream.GetLastMsgForSubject(ctx, "tool.execute.lookup")
				require.ErrorIs(t, readErr, jetstream.ErrMsgNotFound)
			}
			require.NoError(t, replacement.Stop(ctx)) // Includes canceled publication and all input callbacks.
			info, err = consumer.Info(ctx)
			require.NoError(t, err)
			if row.dual || row.publishFailure || row.cancel {
				require.Equal(t, 1, info.NumAckPending)
			} else {
				require.Zero(t, info.NumAckPending)
			}
		})
	}
}
