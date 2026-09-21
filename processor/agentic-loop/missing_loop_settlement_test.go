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
	"github.com/c360studio/semstreams/internal/deliverylane"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
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
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
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
			result, admitted := deliverylane.Consume(
				t.Context(), msg, heartbeatPolicyForTest(t, port.port, port.handler(c)), deliverylane.NewAdmission(nil, nil))
			require.True(t, admitted)
			require.Equal(t, natsclient.DeliveryDecisionTerminate, result.Decision())
			require.Equal(t, int32(1), msg.terms.Load())
			require.Zero(t, msg.acks.Load()+msg.naks.Load())
		})
	}
}

// A payload that decodes but is the wrong registered type is equally permanent.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestWrongPayloadTypeOnHeartbeatLaneTerminatesRatherThanAcking(t *testing.T) {
	t.Parallel()

	c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
	// A ToolResult arriving on the response lane: decodes, wrong type.
	data := baseMessageBytes(t, &agentic.ToolResult{CallID: "call-x", Name: "search", Content: "r"})
	msg := &loopDeliveryOwnerMsg{data: data}
	result, admitted := deliverylane.Consume(
		t.Context(), msg, heartbeatPolicyForTest(t, "agent.response", c.handleResponseMessage), deliverylane.NewAdmission(nil, nil))
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
		result, admitted := deliverylane.Consume(
			t.Context(), msg, heartbeatPolicyForTest(t, "agent.response", c.handleResponseMessage), deliverylane.NewAdmission(nil, nil))
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
		result, admitted := deliverylane.Consume(
			t.Context(), msg, heartbeatPolicyForTest(t, "tool.result", c.handleToolResultMessage), deliverylane.NewAdmission(nil, nil))
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
		decision, err := settleOn(t, c, loopHint)
		return decision, err
	}

	t.Run("finished or foreign loop acknowledges", func(t *testing.T) {
		t.Parallel()
		decision, err := settle(t, map[string]any{"loop_id": terminalLoopID})
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	})

	t.Run("a verdict with no recoverable loop identity terminates as malformed", func(t *testing.T) {
		t.Parallel()
		// A provider-authored call id is not a loop token and no rule
		// template produces one that is, so this payload names no loop any
		// record could answer for. Acknowledging it would be
		// indistinguishable from "that loop finished" — the adopter whose
		// rule echoes a non-canonical loop_id would lose every verdict with
		// no signal saying why. It is malformed input, like an undecodable
		// verdict, and it carries its own metric reason.
		config := DefaultConfig()
		config.ToolCallGovernance.Mode = ToolCallGovernanceModeEnforce
		config.ToolCallGovernance.Timeout = "1s"
		handler := NewMessageHandler(config)
		handler.SetGovernanceDispatcher(NewGovernanceDispatcher(
			config.ToolCallGovernance, nil, discardLogger(), nil))
		c := releaseTestComponent(t, handler)
		c.config = config
		c.loopsBucket = bucket
		c.metrics = getMetrics(nil)

		// Deltas, not absolutes: getMetrics is a package singleton shared by
		// the whole test binary.
		reason := func(label string) float64 {
			return testutil.ToFloat64(
				c.metrics.governanceSubscribeBeforePublishFailures.WithLabelValues(label))
		}
		beforeIdentity, beforeWaiter := reason(verdictDropUnrecoverableIdentity), reason(verdictDropMissingWaiter)
		decision, err := settleOn(t, c, map[string]any{"call_id": "toolu_model_authored"})

		require.Error(t, err)
		require.Equal(t, natsclient.DeliveryDecisionTerminate, decision,
			"an unattributable verdict is malformed input, not a settled loop")
		require.ErrorIs(t, err, ErrNoGovernanceWaiter, "the waiter-miss cause is still carried")
		require.ErrorContains(t, err, "carries no recoverable loop identity")
		require.Equal(t, beforeIdentity+1, reason(verdictDropUnrecoverableIdentity),
			"the identity loss must be countable apart from a waiter miss")
		require.Equal(t, beforeWaiter, reason(verdictDropMissingWaiter),
			"and must not be counted as one")
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
		// A minimal approve-action rule may carry only request_id at the
		// top level. Its grammar is <loopID>:req:<n>:<n>, so the record is
		// still reachable and a live loop still Retries.
		decision, err := settle(t, map[string]any{"request_id": liveLoopID + ":req:2:0"})
		require.Error(t, err)
		require.ErrorIs(t, err, ErrNoGovernanceWaiter)
		require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
	})

	t.Run("the publish-action shape finds the loop under properties", func(t *testing.T) {
		t.Parallel()
		// This is the shape the canonical ADR-039 reject rule actually
		// produces: the `publish` action nests every field under
		// `properties`, and none of the seven templates in
		// docs/operations/17-tool-call-governance.md carries loop_id — so
		// properties.request_id is the ONLY source for an operator using the
		// documented rule set. Without this read, a live loop's rejection
		// would terminate as unattributable and the loop would hang at its
		// governance gate until the timeout rejected it fail-closed.
		decision, err := settle(t, map[string]any{
			"properties": map[string]any{"request_id": liveLoopID + ":req:2:0"},
		})
		require.Error(t, err)
		require.ErrorIs(t, err, ErrNoGovernanceWaiter)
		require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
	})
}

// settleOn drives one verdict through the production seam on a caller-supplied
// component, so a test that needs to read the component's metrics afterwards
// can keep the same wire shape as the table above.
func settleOn(t *testing.T, c *Component, loopHint map[string]any) (natsclient.DeliveryDecision, error) {
	t.Helper()
	payload := map[string]any{
		"decision":     "approved",
		"execution_id": "tool-exec-v1-" + strings.Repeat("a", 52),
	}
	maps.Copy(payload, loopHint)
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	return c.handleToolCallVerdictMessage(t.Context(), data)
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

// The dropped counters mean work this process decided not to do, and the
// response and tool-result lanes were incrementing them on their way to a
// Retry — one reported discard per redelivery for an input nothing had
// discarded. The cancel lane already drew this line
// (signals_dropped_total's help text); this holds all three to it.
//
// Deliberately NOT parallel: loopMetrics counters are process-global, so a
// before/after delta on one is only meaningful when nothing else is moving it.
//
// spec: agentic-loop / A loop absent from process memory is settled from its record
func TestRetriedInputsAreNotCountedAsDrops(t *testing.T) {
	const (
		terminalLoopID = "c1e8b2f3-3d5e-4b6c-9f70-2e3d4c5b6f71"
		liveLoopID     = "b0f7a1e2-2c4d-4a5b-8e6f-1d2c3b4a5e60"
	)
	newComponent := func(t *testing.T, bucket jetstream.KeyValue) *Component {
		t.Helper()
		c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
		c.metrics = getMetrics(nil)
		c.loopsBucket = bucket
		return c
	}
	records := recordLoopBucket{records: map[string]agentic.LoopEntity{
		terminalLoopID: {ID: terminalLoopID, State: agentic.LoopStateComplete},
		liveLoopID:     {ID: liveLoopID, State: agentic.LoopStateExploring},
	}}
	unreadable := recordLoopBucket{getErr: errors.New("kv unavailable")}
	responseBytes := func(t *testing.T, loopID string) []byte {
		t.Helper()
		return baseMessageBytes(t, &agentic.AgentResponse{
			RequestID: loopID + ":req:1", Status: agentic.StatusComplete,
			Message: agentic.ChatMessage{Role: "assistant", Content: "done"},
		})
	}
	toolResultBytes := func(t *testing.T, loopID string) []byte {
		t.Helper()
		return baseMessageBytes(t, &agentic.ToolResult{
			CallID: loopID + ":tool:1", Name: "search", Content: "result", LoopID: loopID,
		})
	}
	// Every label value, not a named one: the point is that no drop of any
	// reason is recorded, including a reason a later change might invent.
	drops := func(t *testing.T, vec *prometheus.CounterVec) float64 {
		t.Helper()
		collected := make(chan prometheus.Metric, 64)
		vec.Collect(collected)
		close(collected)
		total := 0.0
		for metric := range collected {
			var measured dto.Metric
			require.NoError(t, metric.Write(&measured))
			total += measured.GetCounter().GetValue()
		}
		return total
	}
	responseDrops := func(t *testing.T, c *Component) float64 { return drops(t, c.metrics.modelResponsesDropped) }
	toolDrops := func(t *testing.T, c *Component) float64 { return drops(t, c.metrics.toolResultsDropped) }

	t.Run("a stale response is still a counted drop", func(t *testing.T) {
		c := newComponent(t, records)
		before := responseDrops(t, c)
		require.NoError(t, c.handleResponseMessage(t.Context(), responseBytes(t, terminalLoopID)))
		require.Equal(t, float64(1), responseDrops(t, c)-before,
			"an acknowledged stale input is exactly what this counter is for")
	})

	t.Run("a live response retries without a drop", func(t *testing.T) {
		c := newComponent(t, records)
		before := responseDrops(t, c)
		require.Error(t, c.handleResponseMessage(t.Context(), responseBytes(t, liveLoopID)))
		require.Equal(t, before, responseDrops(t, c), "a retried response is not a dropped one")
	})

	t.Run("an unreadable record retries without a drop", func(t *testing.T) {
		c := newComponent(t, unreadable)
		before := responseDrops(t, c)
		require.Error(t, c.handleResponseMessage(t.Context(), responseBytes(t, liveLoopID)))
		require.Equal(t, before, responseDrops(t, c), "an unknown record is not a discarded response")
	})

	t.Run("a stale tool result is still a counted drop", func(t *testing.T) {
		c := newComponent(t, records)
		before := toolDrops(t, c)
		require.NoError(t, c.handleToolResultMessage(t.Context(), toolResultBytes(t, terminalLoopID)))
		require.Equal(t, float64(1), toolDrops(t, c)-before)
	})

	t.Run("a live tool result retries without a drop", func(t *testing.T) {
		c := newComponent(t, records)
		before := toolDrops(t, c)
		require.Error(t, c.handleToolResultMessage(t.Context(), toolResultBytes(t, liveLoopID)))
		require.Equal(t, before, toolDrops(t, c), "a retried tool result is not a dropped one")
	})
}
