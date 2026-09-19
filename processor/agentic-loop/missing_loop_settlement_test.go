package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// recordLoopBucket answers Get from a fixed map so the two opposite
// classifications can be driven through the real production callbacks.
type recordLoopBucket struct {
	jetstream.KeyValue
	records map[string]agentic.LoopEntity
	getErr  error
}

func (b recordLoopBucket) Get(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
	if b.getErr != nil {
		return nil, b.getErr
	}
	entity, ok := b.records[key]
	if !ok {
		return nil, jetstream.ErrKeyNotFound
	}
	data, err := json.Marshal(entity)
	if err != nil {
		return nil, err
	}
	return recordLoopEntry{key: key, value: data}, nil
}

func (recordLoopBucket) Put(context.Context, string, []byte) (uint64, error) { return 1, nil }

type recordLoopEntry struct {
	jetstream.KeyValueEntry
	key   string
	value []byte
}

func (e recordLoopEntry) Key() string    { return e.key }
func (e recordLoopEntry) Value() []byte  { return e.value }
func (recordLoopEntry) Revision() uint64 { return 1 }

func heartbeatPolicyForTest(t *testing.T, port string, handler inputHandler) natsclient.HeartbeatDeliveryPolicy {
	t.Helper()
	policy, err := newLoopHeartbeatDeliveryPolicy(t.Context(), natsclient.StreamConsumerConfig{
		AckWait: 2 * time.Minute, BackOff: []time.Duration{30 * time.Second, 2 * time.Minute}, MaxDeliver: 2,
	}, 15*time.Second, port, handler)
	require.NoError(t, err)
	return policy
}

func baseMessageBytes(t *testing.T, payload message.Payload) []byte {
	t.Helper()
	data, err := json.Marshal(message.NewBaseMessage(payload.Schema(), payload, "test"))
	require.NoError(t, err)
	return data
}

// Bytes that will never decode cannot become successful callback completion.
// Before this, both heartbeat lanes logged and returned nil, which the policy
// read as done and ACKed — the exact log-only-to-ACK shape the change removes.
//
// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestUndecodableHeartbeatLaneInputTerminatesRatherThanAcking(t *testing.T) {
	t.Parallel()

	for name, port := range map[string]struct {
		port    string
		handler func(*Component) inputHandler
	}{
		"agent response": {port: "agent.response", handler: func(c *Component) inputHandler { return c.handleResponseMessage }},
		"tool result":    {port: "tool.result", handler: func(c *Component) inputHandler { return c.handleToolResultMessage }},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
			msg := &loopDeliveryOwnerMsg{data: []byte("{not json")}
			result, admitted := consumeAdmittedDelivery(
				t.Context(), msg, heartbeatPolicyForTest(t, port.port, port.handler(c)), newDeliveryLaneAdmission(nil))
			require.True(t, admitted)
			require.Equal(t, natsclient.DeliveryDecisionTerminate, result.Decision())
			require.Equal(t, int32(1), msg.terms.Load())
			require.Zero(t, msg.acks.Load()+msg.naks.Load())
		})
	}
}

// A payload that decodes but is the wrong registered type is equally permanent.
//
// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestWrongPayloadTypeOnHeartbeatLaneTerminatesRatherThanAcking(t *testing.T) {
	t.Parallel()

	c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
	// A ToolResult arriving on the response lane: decodes, wrong type.
	data := baseMessageBytes(t, &agentic.ToolResult{CallID: "call-x", Name: "search", Content: "r"})
	msg := &loopDeliveryOwnerMsg{data: data}
	result, admitted := consumeAdmittedDelivery(
		t.Context(), msg, heartbeatPolicyForTest(t, "agent.response", c.handleResponseMessage), newDeliveryLaneAdmission(nil))
	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionTerminate, result.Decision())
	require.Equal(t, int32(1), msg.terms.Load())
	require.Zero(t, msg.acks.Load()+msg.naks.Load())
}

// The pair that matters: identical from process memory, opposite settlements.
// A terminal record is the expected settled-drop and ACKs; a live record means
// this process lost the loop and an ACK would destroy the model's work.
//
// spec: agentic-loop / A loop absent from process memory is settled from its record
func TestUncorrelatedResponseSettlesByRecordNotByMemory(t *testing.T) {
	t.Parallel()

	const (
		terminalLoopID = "c1e8b2f3-3d5e-4b6c-9f70-2e3d4c5b6f71"
		liveLoopID     = "b0f7a1e2-2c4d-4a5b-8e6f-1d2c3b4a5e60"
		absentLoopID   = "d2f9c304-4e6f-4c7d-a081-3f4e5d6c7082"
	)
	bucket := recordLoopBucket{records: map[string]agentic.LoopEntity{
		terminalLoopID: {ID: terminalLoopID, State: agentic.LoopStateComplete},
		liveLoopID:     {ID: liveLoopID, State: agentic.LoopStateExploring},
	}}

	settle := func(t *testing.T, loopID string, loopsBucket jetstream.KeyValue) (*loopDeliveryOwnerMsg, natsclient.DeliveryResult) {
		t.Helper()
		c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
		c.loopsBucket = loopsBucket
		// Nothing is tracked in memory: findLoopIDForRequest returns "".
		response := &agentic.AgentResponse{
			RequestID: loopID + ":req:1", Status: agentic.StatusComplete,
			Message: agentic.ChatMessage{Role: "assistant", Content: "done"},
		}
		msg := &loopDeliveryOwnerMsg{data: baseMessageBytes(t, response)}
		result, admitted := consumeAdmittedDelivery(
			t.Context(), msg, heartbeatPolicyForTest(t, "agent.response", c.handleResponseMessage), newDeliveryLaneAdmission(nil))
		require.True(t, admitted)
		return msg, result
	}

	t.Run("terminal record acks", func(t *testing.T) {
		t.Parallel()
		msg, result := settle(t, terminalLoopID, bucket)
		require.Equal(t, natsclient.DeliveryDecisionAck, result.Decision())
		require.Equal(t, int32(1), msg.acks.Load())
		require.Zero(t, msg.naks.Load()+msg.terms.Load())
	})

	t.Run("absent record acks", func(t *testing.T) {
		t.Parallel()
		msg, result := settle(t, absentLoopID, bucket)
		require.Equal(t, natsclient.DeliveryDecisionAck, result.Decision())
		require.Equal(t, int32(1), msg.acks.Load())
	})

	t.Run("live record retries and never acks", func(t *testing.T) {
		t.Parallel()
		msg, result := settle(t, liveLoopID, bucket)
		require.Equal(t, natsclient.DeliveryDecisionRetry, result.Decision())
		require.Equal(t, int32(1), msg.naks.Load())
		require.Zero(t, msg.acks.Load()+msg.terms.Load())
		require.Contains(t, result.Err().Error(), "not held by this process")
	})

	t.Run("unreadable record retries, never assumes stale", func(t *testing.T) {
		t.Parallel()
		msg, result := settle(t, liveLoopID, recordLoopBucket{getErr: errors.New("kv unavailable")})
		require.Equal(t, natsclient.DeliveryDecisionRetry, result.Decision())
		require.Equal(t, int32(1), msg.naks.Load())
		require.Zero(t, msg.acks.Load()+msg.terms.Load())
	})
}

// Same pair on the tool-result lane, where an ACK discards an executor's
// already-completed side effect.
//
// spec: agentic-loop / A loop absent from process memory is settled from its record
func TestUncorrelatedToolResultSettlesByRecordNotByMemory(t *testing.T) {
	t.Parallel()

	const (
		terminalLoopID = "c1e8b2f3-3d5e-4b6c-9f70-2e3d4c5b6f71"
		liveLoopID     = "b0f7a1e2-2c4d-4a5b-8e6f-1d2c3b4a5e60"
	)
	bucket := recordLoopBucket{records: map[string]agentic.LoopEntity{
		terminalLoopID: {ID: terminalLoopID, State: agentic.LoopStateCancelled},
		liveLoopID:     {ID: liveLoopID, State: agentic.LoopStateExploring},
	}}

	settle := func(t *testing.T, loopID string) (*loopDeliveryOwnerMsg, natsclient.DeliveryResult) {
		t.Helper()
		c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
		c.loopsBucket = bucket
		toolResult := &agentic.ToolResult{
			CallID: loopID + ":tool:1", Name: "search", Content: "result", LoopID: loopID,
		}
		msg := &loopDeliveryOwnerMsg{data: baseMessageBytes(t, toolResult)}
		result, admitted := consumeAdmittedDelivery(
			t.Context(), msg, heartbeatPolicyForTest(t, "tool.result", c.handleToolResultMessage), newDeliveryLaneAdmission(nil))
		require.True(t, admitted)
		return msg, result
	}

	t.Run("terminal record acks", func(t *testing.T) {
		t.Parallel()
		msg, result := settle(t, terminalLoopID)
		require.Equal(t, natsclient.DeliveryDecisionAck, result.Decision())
		require.Equal(t, int32(1), msg.acks.Load())
	})

	t.Run("live record retries and never acks", func(t *testing.T) {
		t.Parallel()
		msg, result := settle(t, liveLoopID)
		require.Equal(t, natsclient.DeliveryDecisionRetry, result.Decision())
		require.Equal(t, int32(1), msg.naks.Load())
		require.Zero(t, msg.acks.Load()+msg.terms.Load())
		require.Contains(t, result.Err().Error(), "not held by this process")
	})
}

// A verdict with no waiter was documented as expected-in-normal-operation and
// settled as Retry, which on an unbounded lane is a hot loop for exactly the
// inputs the RecordGovernanceVerdictMissingWaiter counter exists to count. The
// record decides instead: a finished or foreign loop acknowledges, a live one
// is still owed the verdict.
//
// Routing is by execution identity (#1328), which is an opaque digest carrying
// no loop, so the loop comes from the payload: loop_id when the rule echoes it,
// else the RequestID grammar, else the legacy structured call_id. A payload
// with none of the three names no loop and acknowledges.
//
// spec: agentic-loop / A loop absent from process memory is settled from its record
func TestVerdictWithoutWaiterSettlesByRecordNotByWaiterMap(t *testing.T) {
	t.Parallel()

	const (
		terminalLoopID = "c1e8b2f3-3d5e-4b6c-9f70-2e3d4c5b6f71"
		liveLoopID     = "b0f7a1e2-2c4d-4a5b-8e6f-1d2c3b4a5e60"
	)
	bucket := recordLoopBucket{records: map[string]agentic.LoopEntity{
		terminalLoopID: {ID: terminalLoopID, State: agentic.LoopStateComplete},
		liveLoopID:     {ID: liveLoopID, State: agentic.LoopStateExploring},
	}}

	// Every verdict routes on execution_id; the loop hint under test is
	// whatever else the payload carries.
	settle := func(t *testing.T, loopHint map[string]any) (natsclient.DeliveryDecision, error) {
		t.Helper()
		config := DefaultConfig()
		config.ToolCallGovernance.Mode = ToolCallGovernanceModeEnforce
		config.ToolCallGovernance.Timeout = "1s"
		handler := NewMessageHandler(config)
		handler.SetGovernanceDispatcher(NewGovernanceDispatcher(
			config.ToolCallGovernance, nil, discardLogger(), nil))
		c := releaseTestComponent(t, handler)
		c.config = config
		c.loopsBucket = bucket
		payload := map[string]any{
			"decision":     "approved",
			"execution_id": "tool-exec-v1-" + strings.Repeat("a", 52),
		}
		maps.Copy(payload, loopHint)
		data, err := json.Marshal(payload)
		require.NoError(t, err)
		return c.handleToolCallVerdictMessage(t.Context(), data)
	}

	t.Run("finished or foreign loop acknowledges", func(t *testing.T) {
		t.Parallel()
		decision, err := settle(t, map[string]any{"loop_id": terminalLoopID})
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	})

	t.Run("a provider-authored call id with no minted token acknowledges", func(t *testing.T) {
		t.Parallel()
		decision, err := settle(t, map[string]any{"call_id": "toolu_model_authored"})
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	})

	t.Run("live loop is still owed the verdict", func(t *testing.T) {
		t.Parallel()
		decision, err := settle(t, map[string]any{"loop_id": liveLoopID})
		require.Error(t, err)
		require.ErrorIs(t, err, ErrNoGovernanceWaiter)
		require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
	})

	t.Run("a rule that echoes only request_id still finds the loop", func(t *testing.T) {
		t.Parallel()
		// The canonical rule set echoes loop_id, but a minimal rule may
		// carry only request_id. Its grammar is <loopID>:req:<n>:<n>, so
		// the record is still reachable and a live loop still Retries —
		// without this fallback the verdict would Ack and be lost.
		decision, err := settle(t, map[string]any{"request_id": liveLoopID + ":req:2:0"})
		require.Error(t, err)
		require.ErrorIs(t, err, ErrNoGovernanceWaiter)
		require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
	})
}

// CancelLoop refuses for two permanent reasons, and handleSignalMessage maps
// every non-fatal error to Retry, so both used to redeliver against a decision
// that can never change. Neither is transient and neither is a failure — but
// the third case, a loop that is live in another process, still is owed the
// cancel and must not be acknowledged away with them.
//
// spec: agentic-loop / A loop absent from process memory is settled from its record
// Deliberately NOT parallel: loopMetrics counters are process-global, so a
// before/after delta on one is only meaningful when nothing else is moving it.
func TestUncancellableLoopSettlesWithoutRetrying(t *testing.T) {

	const liveElsewhereLoopID = "b0f7a1e2-2c4d-4a5b-8e6f-1d2c3b4a5e60"
	const absentLoopID = "d2f9c304-4e6f-4c7d-a081-3f4e5d6c7082"

	newComponent := func(t *testing.T) *Component {
		t.Helper()
		c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
		c.metrics = getMetrics(nil)
		c.loopsBucket = recordLoopBucket{records: map[string]agentic.LoopEntity{
			liveElsewhereLoopID: {ID: liveElsewhereLoopID, State: agentic.LoopStateExploring},
		}}
		return c
	}
	cancelSignal := func(t *testing.T, loopID string) []byte {
		t.Helper()
		return baseMessageBytes(t, &agentic.UserSignal{
			SignalID: "sig-1", LoopID: loopID, Type: agentic.SignalCancel, UserID: "operator",
		})
	}

	t.Run("already terminal acknowledges without effect", func(t *testing.T) {
		c := newComponent(t)
		loopID, err := c.handler.loopManager.CreateLoop("task-cancel", "general", "model", 3)
		require.NoError(t, err)
		require.NoError(t, c.handler.loopManager.TransitionLoop(loopID, agentic.LoopStateComplete))
		before := testutil.ToFloat64(c.metrics.signalsDropped.WithLabelValues("already_terminal"))

		decision, err := c.handleSignalMessage(t.Context(), cancelSignal(t, loopID))
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision,
			"cancelling a finished loop is idempotent, not a transient failure")
		require.Equal(t, float64(1),
			testutil.ToFloat64(c.metrics.signalsDropped.WithLabelValues("already_terminal"))-before,
			"an effect-free acknowledgement is a declared event")
	})

	t.Run("no durable record acknowledges without effect", func(t *testing.T) {
		c := newComponent(t)
		before := testutil.ToFloat64(c.metrics.signalsDropped.WithLabelValues("stale_loop_id"))

		decision, err := c.handleSignalMessage(t.Context(), cancelSignal(t, absentLoopID))
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
		require.Equal(t, float64(1),
			testutil.ToFloat64(c.metrics.signalsDropped.WithLabelValues("stale_loop_id"))-before)
	})

	t.Run("live in another process is still owed the cancel", func(t *testing.T) {
		c := newComponent(t)
		before := testutil.ToFloat64(c.metrics.signalsDropped.WithLabelValues("stale_loop_id")) +
			testutil.ToFloat64(c.metrics.signalsDropped.WithLabelValues("already_terminal"))

		decision, err := c.handleSignalMessage(t.Context(), cancelSignal(t, liveElsewhereLoopID))
		require.Error(t, err)
		require.Equal(t, natsclient.DeliveryDecisionRetry, decision)

		after := testutil.ToFloat64(c.metrics.signalsDropped.WithLabelValues("stale_loop_id")) +
			testutil.ToFloat64(c.metrics.signalsDropped.WithLabelValues("already_terminal"))
		require.Equal(t, before, after, "a retried signal is not a dropped one")
	})
}
