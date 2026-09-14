//go:build integration

package agenticloop

import (
	"encoding/json"
	"fmt"
	"strings"
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

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestIntegrationTaskRequiredOutputFailureSettlement(t *testing.T) {
	for _, tc := range []struct {
		name         string
		failedOutput string
		replace      bool
	}{
		{name: "created_failure_warm", failedOutput: "agent.created"},
		{name: "initial_request_failure_warm", failedOutput: "agent.request"},
		{name: "created_failure_cold", failedOutput: "agent.created", replace: true},
		{name: "initial_request_failure_cold", failedOutput: "agent.request", replace: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			client := natsclient.NewTestClient(t, natsclient.WithStreams(
				natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>", "tool.>"}},
			))
			discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{
				NATSClient: client.Client, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
			})
			require.NoError(t, err)
			c := discoverable.(*Component)
			// This proof isolates required task publications and loop KV authority;
			// graph birth has its own production-owner tests.
			c.graphWriter = nil
			require.NoError(t, c.initializeKVBuckets(ctx))
			stream, err := client.Client.GetStream(ctx, "AGENT")
			require.NoError(t, err)
			info, err := stream.Info(ctx)
			require.NoError(t, err)
			js, err := client.Client.JetStream()
			require.NoError(t, err)
			// Inject a broker refusal after setup. This is controlled delivery
			// through the production heartbeat owner, not admission of a bounded
			// replay stream or proof of a running consumer's server redelivery.
			faultConfig := info.Config
			faultConfig.Discard = jetstream.DiscardNew
			faultConfig.MaxMsgs = 1
			_, err = js.UpdateStream(ctx, faultConfig)
			require.NoError(t, err)
			if tc.failedOutput == "agent.created" {
				require.NoError(t, client.Client.PublishToStream(ctx, "agent.test.capacity", []byte("occupy the only slot")))
			}

			task := &agentic.TaskMessage{
				LoopID: uuid.NewString(), TaskID: "task-output-" + tc.name,
				Role: "general", Model: "model", Prompt: "work",
			}
			data := settlementEnvelope(t, task)
			policy := task4HeartbeatPolicy(t, "agent.task", c.taskInputHandler(time.Minute))
			admission := newDeliveryLaneAdmission(nil)
			first := &loopDeliveryOwnerMsg{data: data}
			result, admitted := consumeAdmittedDelivery(ctx, first, policy, admission)
			require.True(t, admitted)
			require.Equal(t, natsclient.DeliveryDecisionRetry, result.Decision(), "cause: %v", result.Err())
			require.Equal(t, int32(1), first.naks.Load())
			require.Zero(t, first.acks.Load()+first.terms.Load())
			require.ErrorContains(t, result.Err(), "publish result "+tc.failedOutput+"."+task.LoopID)
			var capacityErr *jetstream.APIError
			require.ErrorAs(t, result.Err(), &capacityErr, "must fail at the actual broker publication")
			require.Equal(t, jetstream.ErrorCode(10077), capacityErr.ErrorCode)
			require.Equal(t, "maximum messages exceeded", capacityErr.Description)

			entry, err := c.loopsBucket.Get(ctx, task.LoopID)
			require.NoError(t, err, "loop birth must precede either required output")
			var persisted agentic.LoopEntity
			require.NoError(t, json.Unmarshal(entry.Value(), &persisted))
			require.NoError(t, persisted.Validate())
			require.Equal(t, task.LoopID, persisted.ID)
			require.Equal(t, task.TaskID, persisted.TaskID)
			require.Equal(t, agentic.LoopStateRunning, persisted.State)
			activeID, active := c.handler.loopManager.HasActiveLoopForTask(task.TaskID)
			require.True(t, active, "redelivery must exercise the warm dedup branch")
			require.Equal(t, task.LoopID, activeID)
			pending, ok := c.pendingTaskResult(task.TaskID, task.LoopID)
			require.True(t, ok, "failed publication must retain the original outputs for retry")
			require.Len(t, pending.PublishedMessages, 2)
			var requestWire, createdWire PublishedMessage
			for _, output := range pending.PublishedMessages {
				switch output.Subject {
				case "agent.request." + task.LoopID:
					requestWire = output
				case "agent.created." + task.LoopID:
					createdWire = output
				}
			}
			require.NotEmpty(t, requestWire.Subject)
			require.NotEmpty(t, createdWire.Subject)
			requestEnvelope, err := c.decoder.Decode(requestWire.Data)
			require.NoError(t, err)
			request, ok := requestEnvelope.Payload().(*agentic.AgentRequest)
			require.True(t, ok)
			require.Equal(t, task.LoopID, request.LoopID)
			require.NotEmpty(t, request.RequestID)

			_, err = stream.GetLastMsgForSubject(ctx, requestWire.Subject)
			require.ErrorIs(t, err, jetstream.ErrMsgNotFound, "model work cannot escape a failed creation publication")
			retainedCreated, createdErr := stream.GetLastMsgForSubject(ctx, createdWire.Subject)
			if tc.failedOutput == "agent.created" {
				require.ErrorIs(t, createdErr, jetstream.ErrMsgNotFound)
			} else {
				require.NoError(t, createdErr, "request failure must follow a real created-event PubAck")
				require.Equal(t, createdWire.Data, retainedCreated.Data)
			}
			faultConfig.MaxMsgs = -1
			_, err = js.UpdateStream(ctx, faultConfig)
			require.NoError(t, err)

			if tc.replace {
				// Replace only the component object, retaining native KV/stream data.
				// Neither object starts consumers; this is not an OS restart claim.
				discoverable, err = NewComponent([]byte(`{}`), component.Dependencies{
					NATSClient: client.Client, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
				})
				require.NoError(t, err)
				c = discoverable.(*Component)
				c.graphWriter = nil
				require.NoError(t, c.initializeKVBuckets(ctx))
				_, active = c.handler.loopManager.HasActiveLoopForTask(task.TaskID)
				require.False(t, active)
				_, ok = c.pendingTaskResult(task.TaskID, task.LoopID)
				require.False(t, ok, "cold replay must not depend on the original pending cache")
				policy = task4HeartbeatPolicy(t, "agent.task", c.taskInputHandler(time.Minute))
				admission = newDeliveryLaneAdmission(nil)
			}

			checkOutputs := func() error {
				var createdSequence uint64
				for _, subject := range []string{createdWire.Subject, requestWire.Subject} {
					stored, readErr := stream.GetLastMsgForSubject(ctx, subject)
					if readErr != nil {
						return fmt.Errorf("task ACK preceded required output %s: %w", subject, readErr)
					}
					decoded, decodeErr := c.decoder.Decode(stored.Data)
					if decodeErr != nil {
						return decodeErr
					}
					switch payload := decoded.Payload().(type) {
					case *agentic.LoopCreatedEvent:
						createdSequence = stored.Sequence
						if subject != createdWire.Subject || payload.LoopID != task.LoopID || payload.TaskID != task.TaskID ||
							payload.Role != task.Role || payload.Model != task.Model {
							return fmt.Errorf("created output changed task correlation: %+v", payload)
						}
					case *agentic.AgentRequest:
						if createdSequence == 0 || createdSequence >= stored.Sequence {
							return fmt.Errorf("model request committed before creation: created=%d request=%d", createdSequence, stored.Sequence)
						}
						if subject != requestWire.Subject || payload.LoopID != task.LoopID || payload.Role != task.Role ||
							payload.Model != task.Model || !strings.HasPrefix(payload.RequestID, task.LoopID+":req:") {
							return fmt.Errorf("request output changed task correlation: %+v", payload)
						}
						if !tc.replace && payload.RequestID != request.RequestID {
							return fmt.Errorf("warm retry changed original request ID")
						}
					default:
						return fmt.Errorf("unexpected output payload on %s: %T", subject, payload)
					}
				}
				return nil
			}
			second := &loopDeliveryOwnerMsg{data: data}
			observed := &terminalMarkerDelivery{Msg: second, beforeAck: checkOutputs}
			result, admitted = consumeAdmittedDelivery(ctx, observed, policy, admission)
			require.True(t, admitted)
			require.NoError(t, result.Err())
			require.Equal(t, natsclient.DeliveryDecisionAck, result.Decision())
			require.Equal(t, int32(1), second.acks.Load())
			require.Zero(t, second.naks.Load()+second.terms.Load())
			require.NoError(t, observed.ackCheckErr, "successful task settlement requires both correlated outputs")
			_, ok = c.pendingTaskResult(task.TaskID, task.LoopID)
			require.False(t, ok)
			entry, err = c.loopsBucket.Get(ctx, task.LoopID)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(entry.Value(), &persisted))
			require.Equal(t, task.LoopID, persisted.ID)
			require.Equal(t, task.TaskID, persisted.TaskID)
			require.Equal(t, agentic.LoopStateRunning, persisted.State)
		})
	}
}
