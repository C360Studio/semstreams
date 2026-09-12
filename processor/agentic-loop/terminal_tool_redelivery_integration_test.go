//go:build integration

package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// terminalToolLostAck models only the transport's lost first ACK after durable
// terminal work. It never forwards that ACK; the real server retains the source.
type terminalToolLostAck struct{ *terminalMarkerDelivery }

func (m *terminalToolLostAck) Ack() error {
	m.acks++
	if m.beforeAck != nil {
		m.ackCheckErr = m.beforeAck()
	}
	return errors.New("injected lost terminal-tool source ACK")
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
// spec: agentic-loop / Loop recovery is lane-specific and read-through
// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestIntegrationTerminalToolResultAppliedAfterReplacement(t *testing.T) {
	for _, stopLoop := range []bool{true, false} {
		name, terminalPrefix := "max_iterations", "agent.failed."
		if stopLoop {
			name, terminalPrefix = "stop_loop", "agent.complete."
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			tc := natsclient.NewTestClient(t, natsclient.WithStreams(
				natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>", "tool.>"}},
			))
			stream, err := tc.Client.GetStream(ctx, "AGENT")
			require.NoError(t, err)
			loopID, suffix := uuid.NewString(), "terminal-tool-"+name
			requestID := loopID + ":req:" + uuid.NewString()
			calls := []agentic.ToolCall{{ID: "same-provider-id", Name: "search"}, {ID: "same-provider-id", Name: "search"}}
			require.NoError(t, stampToolExecutionCorrelation(requestID, calls))
			results := make([]agentic.ToolResult, len(calls))
			for i, call := range calls {
				results[i] = agentic.ToolResult{LoopID: loopID, RequestID: call.RequestID, ExecutionID: call.ExecutionID,
					CallID: call.ID, CallOrdinal: call.CallOrdinal, Name: call.Name, TraceID: call.TraceID, Content: "retained answer"}
			}
			results[1].StopLoop = stopLoop
			entity := agentic.NewLoopEntity(loopID, "terminal-tool-task", "general", "model", 1)
			entity.State, entity.Iterations = agentic.LoopStateExecuting, entity.MaxIterations
			entity.PendingToolResults = map[string]agentic.ToolResult{results[0].ExecutionID: results[0]}
			request := &agentic.AgentRequest{LoopID: loopID, RequestID: requestID, Role: entity.Role, Model: entity.Model,
				Messages: []agentic.ChatMessage{{Role: "user", Content: "finish this task"}}}
			response := &agentic.AgentResponse{RequestID: requestID, Status: agentic.StatusToolCall,
				Message: agentic.ChatMessage{Role: "assistant", ToolCalls: calls}}
			wire := settlementEnvelope(t, &results[1])
			settled := make(chan *terminalMarkerDelivery, 2)
			observe := func(c *Component, loseAck bool) {
				c.consumeStream = func(setupCtx, ownerCtx context.Context, owner natsclient.PortConsumerContext,
					cfg natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg),
				) (jetstream.ConsumeContext, error) {
					return tc.Client.ConsumeStreamWithConfigContexts(setupCtx, ownerCtx, owner, cfg,
						func(msgCtx context.Context, msg jetstream.Msg) {
							if owner.Port != "tool.result" {
								handler(msgCtx, msg)
								return
							}
							observed := &terminalMarkerDelivery{Msg: msg, beforeAck: func() error {
								stored, found, err := c.readLoopEntity(msgCtx, loopID)
								if err != nil || !found || !stored.State.IsTerminal() || stored.PendingToolResults[results[1].ExecutionID].Content != results[1].Content {
									return fmt.Errorf("source ACK preceded exact result and final marker: found=%v err=%v", found, err)
								}
								_, err = stream.GetLastMsgForSubject(msgCtx, terminalPrefix+loopID)
								return err
							}}
							if loseAck {
								handler(msgCtx, &terminalToolLostAck{observed})
							} else {
								handler(msgCtx, observed)
							}
							settled <- observed
						})
				}
			}
			wait := func(budget time.Duration) *terminalMarkerDelivery {
				t.Helper()
				timer := time.NewTimer(budget)
				defer timer.Stop()
				select {
				case observed := <-settled:
					return observed
				case <-timer.C:
					t.Fatal("started terminal-tool owner did not return")
					return nil
				}
			}
			first := newTerminalMarkerProcess(t, tc.Client, suffix)
			first.initializeKVBucketsInput = func(initCtx context.Context) error {
				if err := first.initializeKVBuckets(initCtx); err != nil {
					return err
				}
				// The real retained current request, originating batch and earlier
				// result describe tools in flight at the existing budget boundary.
				if _, err := first.loopsBucket.Put(initCtx, loopID, settlementLoopRecord(t, entity)); err != nil {
					return err
				}
				if err := tc.Client.PublishToStream(initCtx, "agent.request."+loopID, settlementEnvelope(t, request)); err != nil {
					return err
				}
				return tc.Client.PublishToStream(initCtx, "agent.response."+requestID, settlementEnvelope(t, response))
			}
			observe(first, true)
			require.NoError(t, first.Start(ctx))
			require.NoError(t, tc.Client.PublishToStream(ctx, "tool.result."+results[1].ExecutionID, wire))
			lost := wait(5 * time.Second)
			require.Equal(t, 1, lost.acks, "the injected transport failure must follow an attempted ACK")
			require.Zero(t, lost.naks+lost.terms)
			require.NoError(t, lost.ackCheckErr)
			before, err := lost.Metadata()
			require.NoError(t, err)
			consumer, err := stream.Consumer(ctx, before.Consumer)
			require.NoError(t, err)
			info, err := consumer.Info(ctx)
			require.NoError(t, err)
			require.Equal(t, 1, info.Config.MaxAckPending)
			require.Equal(t, 30*time.Second, info.Config.BackOff[0])
			require.Equal(t, 1, info.NumAckPending)
			require.Less(t, info.AckFloor.Stream, before.Sequence.Stream)
			marker, err := first.loopsBucket.Get(ctx, loopID)
			require.NoError(t, err)
			completion, err := first.loopsBucket.Get(ctx, "COMPLETE_"+loopID)
			require.NoError(t, err)
			require.Greater(t, marker.Revision(), completion.Revision())
			published, err := stream.GetLastMsgForSubject(ctx, terminalPrefix+loopID)
			require.NoError(t, err)
			var final agentic.LoopEntity
			require.NoError(t, json.Unmarshal(marker.Value(), &final))
			require.Equal(t, entity.MaxIterations, final.Iterations)
			require.Equal(t, results[1], final.PendingToolResults[results[1].ExecutionID])
			if stopLoop {
				require.Equal(t, agentic.LoopStateComplete, final.State)
				require.Equal(t, results[1].Content, final.Result)
			} else {
				require.Equal(t, agentic.LoopStateFailed, final.State)
				require.Len(t, final.PendingToolResults, len(calls))
				var failure agentic.LoopFailedEvent
				require.NoError(t, json.Unmarshal(completion.Value(), &failure))
				require.Equal(t, "max_iterations", failure.Reason)
			}
			stopCtx, cancelStop := context.WithTimeout(ctx, 5*time.Second)
			defer cancelStop()
			require.NoError(t, first.Stop(stopCtx))
			replacement := newTerminalMarkerProcess(t, tc.Client, suffix)
			observe(replacement, false)
			require.NoError(t, replacement.Start(ctx))
			redelivered := wait(45 * time.Second)
			after, err := redelivered.Metadata()
			require.NoError(t, err)
			require.Equal(t, before.Sequence.Stream, after.Sequence.Stream)
			require.Greater(t, after.NumDelivered, before.NumDelivered)
			require.Equal(t, wire, redelivered.Data())
			require.Equal(t, 1, redelivered.acks)
			require.Zero(t, redelivered.naks+redelivered.terms)
			require.NoError(t, redelivered.ackCheckErr)
			require.Eventually(t, func() bool {
				info, err := consumer.Info(ctx)
				return err == nil && info.NumAckPending == 0 && info.AckFloor.Stream >= after.Sequence.Stream
			}, 5*time.Second, 10*time.Millisecond)
			unchanged, err := replacement.loopsBucket.Get(ctx, loopID)
			require.NoError(t, err)
			require.Equal(t, marker.Revision(), unchanged.Revision(), "applied replay must not rewrite the final marker")
			latest, err := stream.GetLastMsgForSubject(ctx, terminalPrefix+loopID)
			require.NoError(t, err)
			require.Equal(t, published.Sequence, latest.Sequence, "exact applied proof must not repeat terminal output")
			_, err = replacement.handler.GetLoop(loopID)
			require.Error(t, err, "applied replay must not reconstruct terminal process state")
		})
	}
}
