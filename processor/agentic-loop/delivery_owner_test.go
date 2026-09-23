package agenticloop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/deliverylane"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// errKVUnavailable is the shared stand-in for a KV bucket that will not accept
// a write, used by every settlement test that drives a persistence failure.
var errKVUnavailable = errors.New("kv unavailable")

type loopDeliveryOwnerMsg struct {
	data        []byte
	dataCalls   atomic.Int32
	heartbeats  atomic.Int32
	settlement  atomic.Int32
	acks        atomic.Int32
	naks        atomic.Int32
	nakDelays   atomic.Int32
	terms       atomic.Int32
	metadata    atomic.Int32
	metadataErr error
}

func (m *loopDeliveryOwnerMsg) Data() []byte       { m.dataCalls.Add(1); return m.data }
func (*loopDeliveryOwnerMsg) Subject() string      { return "agent.task.test" }
func (*loopDeliveryOwnerMsg) Reply() string        { return "" }
func (*loopDeliveryOwnerMsg) Headers() nats.Header { return nil }
func (m *loopDeliveryOwnerMsg) Metadata() (*jetstream.MsgMetadata, error) {
	m.metadata.Add(1)
	if m.metadataErr != nil {
		return nil, m.metadataErr
	}
	return &jetstream.MsgMetadata{NumDelivered: 1}, nil
}
func (m *loopDeliveryOwnerMsg) Ack() error                    { m.acks.Add(1); m.settlement.Add(1); return nil }
func (*loopDeliveryOwnerMsg) DoubleAck(context.Context) error { return nil }
func (m *loopDeliveryOwnerMsg) Nak() error                    { m.naks.Add(1); m.settlement.Add(1); return nil }
func (m *loopDeliveryOwnerMsg) NakWithDelay(time.Duration) error {
	m.naks.Add(1)
	m.nakDelays.Add(1)
	m.settlement.Add(1)
	return nil
}
func (m *loopDeliveryOwnerMsg) InProgress() error           { m.heartbeats.Add(1); return nil }
func (m *loopDeliveryOwnerMsg) Term() error                 { m.terms.Add(1); m.settlement.Add(1); return nil }
func (m *loopDeliveryOwnerMsg) TermWithReason(string) error { return m.Term() }

// recordingLoopBucket fails every write until its error is cleared, and then
// records the keys it is asked to write. Both halves matter: the failure drives
// the classification, and the key list is how the counterfactual below observes
// what a Retry would have lost.
// failPrefix narrows the failure to one key family, which is how a test can
// fail the terminal RECORD write while every other key on the same path
// succeeds — the only way to tell "wrote the loop key and ACKed" apart from
// "wrote both".
//
// It carries real revision semantics — Create refuses an existing key, Update
// refuses a moved one — because since #1330 the loop record is written under
// compare-and-swap and a fake that answered any revision would let a
// last-writer-wins regression pass.
type recordingLoopBucket struct {
	jetstream.KeyValue
	mu         sync.Mutex
	fail       error
	failPrefix string
	keys       []string
	values     map[string][]byte
	revisions  map[string]uint64
	// seq is monotonic across the bucket's life, independent of the recorded
	// key list. A revision derived from len(keys) would silently stand still
	// whenever a fixture reset that list, which is exactly the case a
	// compare-and-swap test needs to move.
	seq uint64
}

func (b *recordingLoopBucket) Put(_ context.Context, key string, value []byte) (uint64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.writeLocked(key, value)
}

func (b *recordingLoopBucket) Create(_ context.Context, key string, value []byte, _ ...jetstream.KVCreateOpt) (uint64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.values[key]; exists {
		return 0, jetstream.ErrKeyExists
	}
	return b.writeLocked(key, value)
}

func (b *recordingLoopBucket) Update(_ context.Context, key string, value []byte, revision uint64) (uint64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.revisions[key] != revision {
		// The wrong-last-sequence API error, which is what NATS answers a
		// compare-and-swap whose key has moved.
		return 0, jetstream.ErrKeyExists
	}
	return b.writeLocked(key, value)
}

func (b *recordingLoopBucket) Get(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	value, ok := b.values[key]
	if !ok {
		return nil, jetstream.ErrKeyNotFound
	}
	return recordLoopEntry{key: key, value: append([]byte(nil), value...), revision: b.revisions[key]}, nil
}

func (b *recordingLoopBucket) writeLocked(key string, value []byte) (uint64, error) {
	if b.fail != nil && strings.HasPrefix(key, b.failPrefix) {
		return 0, b.fail
	}
	b.keys = append(b.keys, key)
	if b.values == nil {
		b.values = map[string][]byte{}
	}
	if b.revisions == nil {
		b.revisions = map[string]uint64{}
	}
	b.values[key] = append([]byte(nil), value...)
	b.seq++
	b.revisions[key] = b.seq
	return b.revisions[key], nil
}

// revisionOf reports the revision a key currently holds, which is what a
// reader of this record would compare-and-swap against.
func (b *recordingLoopBucket) revisionOf(key string) uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.revisions[key]
}

func (b *recordingLoopBucket) value(key string) ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	value, ok := b.values[key]
	return value, ok
}

func (b *recordingLoopBucket) heal() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fail = nil
}

func (b *recordingLoopBucket) arm(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fail = err
}

// seedLoopRecord writes the loop's FIRST record through the production birth
// path, which is what leaves the component holding the revision every later
// compare-and-swap compares against (#1330). A fixture that skips it is
// modelling a process that never created the loop, where refusing to write is
// the correct answer — so every warm fixture has to do what a birth does.
func seedLoopRecord(t *testing.T, c *Component, loopID string) {
	t.Helper()
	require.NoError(t, c.createLoopState(t.Context(), loopID))
	// The birth write is fixture setup, not delivery behaviour, and written()
	// exists to observe delivery behaviour.
	if bucket, ok := c.loopsBucket.(*recordingLoopBucket); ok {
		bucket.resetWritten()
	}
}

// resetWritten forgets the key list without forgetting the records. Fixture
// setup (the birth write seedLoopRecord performs) is not delivery behaviour,
// and written() exists to observe delivery behaviour.
func (b *recordingLoopBucket) resetWritten() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.keys = nil
}

func (b *recordingLoopBucket) written() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.keys...)
}

// A persistence failure arrives AFTER the handler has already moved the loop,
// so the delivery that produced it can no longer be replayed: this is the R1
// case, and the classification is Quarantine rather than Retry.
//
// The response subtest carries the counterfactual that makes the rule
// load-bearing rather than stylistic. A complete model response drives the loop
// to complete in memory and builds its completion record; the KV write then
// fails. Under the retired Retry classification the redelivery meets
// HandleModelResponse's terminal guard (handlers.go:1322-1327), which returns an
// empty result — so the second attempt writes the loop key, writes no
// COMPLETE_<loopID>, publishes nothing, and ACKs. The completion is gone with
// the delivery that carried it. The second half of this test drives exactly that
// redelivery with a healed bucket and asserts the completion record is absent,
// which is what a Retry would have settled as done.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestResponseAndToolResultPersistenceFailureCannotAck(t *testing.T) {
	newPolicy := func(t *testing.T, port string, handler inputHandler) natsclient.HeartbeatDeliveryPolicy {
		t.Helper()
		policy, err := newLoopHeartbeatDeliveryPolicy(t.Context(), natsclient.StreamConsumerConfig{
			AckWait: 2 * time.Minute, BackOff: []time.Duration{30 * time.Second, 2 * time.Minute}, MaxDeliver: 2,
		}, 15*time.Second, port, handler)
		require.NoError(t, err)
		return policy
	}

	t.Run("agent response", func(t *testing.T) {
		handler := NewMessageHandler(DefaultConfig())
		loopID, err := handler.loopManager.CreateLoop("task-response", "general", "model", 3)
		require.NoError(t, err)
		requestID := handler.loopManager.GenerateRequestID(loopID)
		handler.loopManager.TrackRequest(requestID, loopID)
		c := releaseTestComponent(t, handler)
		bucket := &recordingLoopBucket{}
		c.loopsBucket = bucket
		seedLoopRecord(t, c, loopID)
		bucket.arm(errors.New("kv unavailable"))
		response := &agentic.AgentResponse{
			RequestID: requestID, Status: agentic.StatusComplete,
			Message: agentic.ChatMessage{Role: "assistant", Content: "done"},
		}
		data, err := json.Marshal(message.NewBaseMessage(response.Schema(), response, "test"))
		require.NoError(t, err)
		msg := &loopDeliveryOwnerMsg{data: data}
		admission := deliverylane.NewAdmission(nil, nil)
		policy := newPolicy(t, "agent.response", c.handleResponseMessage)
		result, admitted := deliverylane.Consume(t.Context(), msg, policy, admission)
		require.True(t, admitted)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision())
		require.True(t, result.OwnerStopRequired())
		require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load(),
			"a quarantined delivery attempts no terminal method at all")
		// The terminal owner's first step is the marker's Create (#1362), so
		// that is the write this unavailable bucket refuses.
		require.Contains(t, result.Err().Error(), "create terminal marker")

		// The lane is latched, so this owner runs no further work on it.
		redelivery := &loopDeliveryOwnerMsg{data: data}
		_, readmitted := deliverylane.Consume(t.Context(), redelivery, policy, admission)
		require.False(t, readmitted, "a latched lane admitted more work after a quarantined delivery")
		require.Zero(t, redelivery.dataCalls.Load())

		// The counterfactual: what the retired Retry would have settled. Same
		// loop, same bytes, healthy KV, a lane that had not latched.
		bucket.heal()
		retried := &loopDeliveryOwnerMsg{data: data}
		retriedResult, admitted := deliverylane.Consume(
			t.Context(), retried, policy, deliverylane.NewAdmission(nil, nil))
		require.True(t, admitted)
		require.Equal(t, natsclient.DeliveryDecisionAck, retriedResult.Decision())
		require.Equal(t, []string{loopID}, bucket.written(),
			"the redelivered response persisted the loop key and no COMPLETE_ record: "+
				"a Retry would have acknowledged a completion nothing downstream can read")
	})

	t.Run("tool result", func(t *testing.T) {
		handler := NewMessageHandler(DefaultConfig())
		loopID, err := handler.loopManager.CreateLoop("task-tool", "general", "model", 3)
		require.NoError(t, err)
		_, err = handler.HandleModelResponse(t.Context(), loopID, agentic.AgentResponse{
			RequestID: "request-tool", Status: "tool_call",
			Message: agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{{ID: "call-tool", Name: "search"}}},
		})
		require.NoError(t, err)
		c := releaseTestComponent(t, handler)
		c.loopsBucket = failingLoopBucket{err: errors.New("kv unavailable")}
		// The revision a birth would have left behind, so the write is what
		// fails here rather than the missing observation that precedes it.
		c.rememberLoopRevision(loopID, 1)
		toolResult := &agentic.ToolResult{
			RequestID: "request-tool", ExecutionID: deriveToolExecutionID("request-tool", "call-tool", 1),
			CallID: "call-tool", CallOrdinal: 1, Name: "search", Content: "result",
		}
		data, err := json.Marshal(message.NewBaseMessage(toolResult.Schema(), toolResult, "test"))
		require.NoError(t, err)
		msg := &loopDeliveryOwnerMsg{data: data}
		result, admitted := deliverylane.Consume(t.Context(), msg, newPolicy(t, "tool.result", c.handleToolResultMessage), deliverylane.NewAdmission(nil, nil))
		require.True(t, admitted)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision())
		require.True(t, result.OwnerStopRequired())
		require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())
		require.Contains(t, result.Err().Error(), "persist loop state")
	})
}

// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestLoopUnavailableDeliveryMetadataQuarantinesAndStopsExactOwner(t *testing.T) {
	retry, err := natsclient.DelayedDeliveryRetry(30 * time.Second)
	require.NoError(t, err)
	var workCalls atomic.Int32
	policy, err := natsclient.ValidateHeartbeatDeliveryPolicy(
		t.Context(),
		natsclient.StreamConsumerConfig{BackOff: []time.Duration{30 * time.Second, 2 * time.Minute}},
		15*time.Second,
		retry,
		func(context.Context, []byte) (natsclient.DeliveryDecision, error) {
			workCalls.Add(1)
			return natsclient.DeliveryDecisionAck, nil
		},
	)
	require.NoError(t, err)
	admission := deliverylane.NewAdmission(nil, nil)
	metadataCause := errors.New("metadata unavailable")
	msg := &loopDeliveryOwnerMsg{data: []byte("must-not-run"), metadataErr: metadataCause}

	result, admitted := deliverylane.Consume(t.Context(), msg, policy, admission)

	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision())
	require.True(t, result.Quarantined())
	require.True(t, result.OwnerStopRequired())
	require.Contains(t, result.Err().Error(), "delivery_metadata_unavailable")
	require.ErrorIs(t, result.Cause(), metadataCause)
	require.Zero(t, msg.dataCalls.Load())
	require.Zero(t, workCalls.Load())
	require.Zero(t, msg.heartbeats.Load())
	require.Zero(t, msg.settlement.Load())

	handle := &loopPolicyHandle{closed: make(chan struct{})}
	binding := deliverylane.NewBinding(handle)
	ctx, cancel := context.WithCancel(t.Context())
	c := &Component{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	deliverylane.Observe(ctx, binding, admission, func(result natsclient.DeliveryResult) {
		c.logger.Error("Loop delivery ownership lost", "port", "agent.task", "error", result.Err())
	})
	require.Eventually(t, func() bool { return handle.drains.Load() == 1 }, time.Second, time.Millisecond)

	_, admitted = deliverylane.Consume(t.Context(), msg, policy, admission)
	require.False(t, admitted)
	require.Equal(t, int32(1), msg.metadata.Load(), "closed admission must not inspect another delivery")
	binding.Drain()
	require.Equal(t, int32(1), handle.drains.Load(), "fatal and ordinary stop share exact drain-once authority")
	cancel()
	<-binding.Done()
}

// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestLoopSetupWiresMetadataFailureToAcquiredOwner(t *testing.T) {
	handles := []*loopPolicyHandle{
		{closed: make(chan struct{})},
		{closed: make(chan struct{})},
	}
	callbacks := make([]func(context.Context, jetstream.Msg), 0, len(handles))
	var workCalls atomic.Int32
	c := &Component{
		config: DefaultConfig(), logger: slog.New(slog.NewTextHandler(io.Discard, nil)), started: true, startTime: time.Now(),
		waitForStreamInput: func(context.Context, string) error { return nil },
		consumeStream: func(_ context.Context, _ context.Context, _ natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, handler func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			index := len(callbacks)
			callbacks = append(callbacks, handler)
			return handles[index], nil
		},
	}
	c.trajectoryAuditHealth.latch("trajectory audit degraded")
	require.Equal(t, 1, c.Health().ErrorCount)

	ctx, cancel := context.WithCancel(t.Context())
	for _, portName := range []string{"agent.task", "agent.response"} {
		port, err := (component.PortDefinition{
			Name:     portName,
			Config:   component.JetStreamPort{StreamName: "AGENT", Subjects: []string{portName + ".>"}},
			Required: true,
		}).Resolve(component.DirectionInput)
		require.NoError(t, err)
		require.NoError(t, c.setupConsumer(
			ctx, ctx, port, portName+".>", func(context.Context, []byte) error {
				workCalls.Add(1)
				return nil
			},
			nil,
		))
	}
	require.Len(t, callbacks, 2)
	require.Len(t, c.consumers, 2)

	firstCause := errors.New("first metadata unavailable")
	first := &loopDeliveryOwnerMsg{metadataErr: firstCause}
	callbacks[0](ctx, first)
	firstHealth := c.Health()
	require.False(t, firstHealth.Healthy)
	require.Equal(t, "delivery ownership lost", firstHealth.Status,
		"owner loss must take precedence over trajectory degradation")
	require.Equal(t, "delivery_metadata_unavailable: "+firstCause.Error(), firstHealth.LastError)
	require.NotContains(t, firstHealth.LastError, "trajectory audit degraded")
	require.Equal(t, 2, firstHealth.ErrorCount, "owner loss adds exactly one error to existing trajectory degradation")
	require.Eventually(t, func() bool { return handles[0].drains.Load() == 1 }, time.Second, time.Millisecond)
	require.Zero(t, handles[1].drains.Load(), "first fatal must not drain another owner")

	secondCause := errors.New("later metadata unavailable")
	second := &loopDeliveryOwnerMsg{metadataErr: secondCause}
	callbacks[1](ctx, second)
	require.Eventually(t, func() bool { return handles[1].drains.Load() == 1 }, time.Second, time.Millisecond)
	secondHealth := c.Health()
	require.Equal(t, "delivery ownership lost", secondHealth.Status)
	require.Equal(t, firstHealth.LastError, secondHealth.LastError, "the first fatal cause must remain sticky")
	require.NotContains(t, secondHealth.LastError, secondCause.Error())
	require.Equal(t, 2, secondHealth.ErrorCount, "later fatal owner loss must not recount")
	require.Equal(t, int32(1), handles[0].drains.Load(), "later fatal must not redrain the first owner")

	callbacks[0](ctx, first)
	require.Equal(t, int32(1), first.metadata.Load(), "closed owner must refuse another delivery before metadata access")
	require.Zero(t, workCalls.Load())
	for index, msg := range []*loopDeliveryOwnerMsg{first, second} {
		require.Zero(t, msg.dataCalls.Load(), "message %d must not expose data to work", index)
		require.Zero(t, msg.heartbeats.Load(), "message %d must not heartbeat", index)
		require.Zero(t, msg.settlement.Load(), "message %d must not settle", index)
		require.Equal(t, int32(1), handles[index].drains.Load(), "only the exact owner handle drains once")
	}

	cancel()
	for _, binding := range c.consumers {
		<-binding.Done()
	}
}

// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestLoopProductionCallbacksTerminateMalformedNonHeartbeatInputs(t *testing.T) {
	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{
		NATSClient: &natsclient.Client{}, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	c := discoverable.(*Component)
	verdicts := &settlementVerdictDispatcher{received: make(chan string, 2)}
	c.handler.SetGovernanceDispatcher(verdicts)
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	callbacks := make(map[string]func(context.Context, jetstream.Msg))
	c.consumeStream = func(_ context.Context, _ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		callbacks[owner.Port] = callback
		return &loopPolicyHandle{closed: make(chan struct{})}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, c.setupSubscriptions(ctx, ctx))

	for _, port := range []string{"agent.signal", "agent.approval_response", "agent.toolcall.approved", "agent.toolcall.rejected"} {
		callback, ok := callbacks[port]
		require.True(t, ok, "production setup did not bind %s", port)
		msg := &loopSettlementMsg{data: []byte("{")}
		callback(ctx, msg)
		require.Zero(t, msg.acks.Load(), "%s must not ACK malformed input", port)
		require.Zero(t, msg.naks.Load(), "%s immutable malformed input must not retry", port)
		require.Equal(t, int32(1), msg.terms.Load(), "%s immutable malformed input must terminate", port)
	}
	for _, row := range []struct {
		port     string
		decision string
		callID   string
	}{
		{port: "agent.toolcall.approved", decision: "approved", callID: "call-approved"},
		{port: "agent.toolcall.rejected", decision: "rejected", callID: "call-rejected"},
	} {
		data := []byte(`{"decision":"` + row.decision + `","execution_id":"` + row.callID + `"}`)
		msg := &loopSettlementMsg{data: data}
		callbacks[row.port](ctx, msg)
		require.Equal(t, int32(1), msg.acks.Load())
		require.Zero(t, msg.naks.Load()+msg.terms.Load())
		require.Equal(t, row.decision+":"+row.callID, <-verdicts.received)
	}

	cancel()
	for _, binding := range c.consumers {
		<-binding.Done()
	}
}

type settlementVerdictDispatcher struct{ received chan string }

func (*settlementVerdictDispatcher) Propose(context.Context, string, string, []agentic.ToolCall) (DispatcherResult, error) {
	return DispatcherResult{}, nil
}
func (d *settlementVerdictDispatcher) HandleVerdict(decision, callID string, _ VerdictPayload) (natsclient.DeliveryDecision, error) {
	d.received <- decision + ":" + callID
	return natsclient.DeliveryDecisionAck, nil
}
func (*settlementVerdictDispatcher) Mode() string { return "enforce" }

// spec: agentic-loop / Loop input classes settle after owner-specific durable done
// scenario: Approval handler panics
func TestLoopApprovalPanicProductionCallbackQuarantinesExactOwner(t *testing.T) {
	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{
		NATSClient: &natsclient.Client{}, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.started = true
	c.startTime = time.Now()
	loopID := c.handler.loopManager.GenerateLoopID()
	c.handler.loopManager = nil
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	callbacks := make(map[string]func(context.Context, jetstream.Msg))
	handles := make(map[string]*loopPolicyHandle)
	c.consumeStream = func(_ context.Context, _ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		handle := &loopPolicyHandle{closed: make(chan struct{})}
		callbacks[owner.Port] = callback
		handles[owner.Port] = handle
		return handle, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, c.setupSubscriptions(ctx, ctx))

	response := &agentic.ApprovalResponse{
		LoopID: loopID, CallID: "call-panic", Decision: agentic.ApprovalDecisionApprove,
		ApprovedBy: "operator", DecidedAt: time.Now().UTC(),
	}
	data, err := json.Marshal(message.NewBaseMessage(response.Schema(), response, "test"))
	require.NoError(t, err)
	msg := &loopSettlementMsg{data: data}
	callbacks["agent.approval_response"](ctx, msg)

	require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())
	require.Eventually(t, func() bool { return handles["agent.approval_response"].drains.Load() == 1 }, time.Second, time.Millisecond)
	for port, handle := range handles {
		if port != "agent.approval_response" {
			require.Zero(t, handle.drains.Load(), "approval panic drained unrelated owner %s", port)
		}
	}
	health := c.Health()
	require.False(t, health.Healthy)
	require.Equal(t, "delivery ownership lost", health.Status)
	require.Contains(t, health.LastError, "approval response handler panicked")

	// The same lane, a second delivery — what a drained handle flushes. The
	// latch must refuse it: no work, no terminal method, and no report of a
	// settlement failure, because nothing was settled. The refusal branch is
	// the settlement lane's early return, which is invisible to every test
	// that replays into a DIFFERENT lane.
	logs := &bytes.Buffer{}
	c.logger = slog.New(slog.NewTextHandler(logs, nil))
	replay := &loopSettlementMsg{data: data}
	callbacks["agent.approval_response"](ctx, replay)
	require.Zero(t, replay.acks.Load()+replay.naks.Load()+replay.terms.Load(),
		"a latched lane attempted a terminal method")
	require.NotContains(t, logs.String(), "Message delivery did not settle cleanly",
		"a refused delivery settled nothing, so it must not be reported as a settlement failure")

	cancel()
	for _, binding := range c.consumers {
		<-binding.Done()
	}
}

// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestLoopCancellationUnknownPublicationQuarantinesWithoutReleasingTransientState(t *testing.T) {
	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{
		NATSClient: &natsclient.Client{}, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.started = true
	c.startTime = time.Now()
	loopID, err := c.handler.loopManager.CreateLoop("task-cancel", "general", "model", 3)
	require.NoError(t, err)
	_, err = c.handler.trajectoryManager.startTrajectory(loopID)
	require.NoError(t, err)
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	callbacks := make(map[string]func(context.Context, jetstream.Msg))
	handles := make(map[string]*loopPolicyHandle)
	c.consumeStream = func(_ context.Context, _ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		handle := &loopPolicyHandle{closed: make(chan struct{})}
		callbacks[owner.Port] = callback
		handles[owner.Port] = handle
		return handle, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, c.setupSubscriptions(ctx, ctx))
	signal := &agentic.UserSignal{
		SignalID: "signal-cancel", Type: agentic.SignalCancel, LoopID: loopID,
		UserID: "operator", Timestamp: time.Now().UTC(),
	}
	data, err := json.Marshal(message.NewBaseMessage(signal.Schema(), signal, "test"))
	require.NoError(t, err)
	msg := &loopSettlementMsg{data: data}
	callbacks["agent.signal"](ctx, msg)

	require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())
	require.Eventually(t, func() bool { return handles["agent.signal"].drains.Load() == 1 }, time.Second, time.Millisecond)
	_, err = c.handler.trajectoryManager.getTrajectory(loopID)
	require.NoError(t, err, "unknown terminal publication released the loop trajectory")
	require.Contains(t, c.Health().LastError, "unknown durability")
	for port, handle := range handles {
		if port != "agent.signal" {
			require.Zero(t, handle.drains.Load(), "cancellation failure drained unrelated owner %s", port)
		}
	}

	cancel()
	for _, binding := range c.consumers {
		<-binding.Done()
	}
}

type loopSettlementMsg struct {
	data  []byte
	acks  atomic.Int32
	naks  atomic.Int32
	terms atomic.Int32
}

func (m *loopSettlementMsg) Data() []byte                            { return m.data }
func (*loopSettlementMsg) Subject() string                           { return "loop.test" }
func (*loopSettlementMsg) Reply() string                             { return "" }
func (*loopSettlementMsg) Headers() nats.Header                      { return nil }
func (*loopSettlementMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (m *loopSettlementMsg) Ack() error                              { m.acks.Add(1); return nil }
func (*loopSettlementMsg) DoubleAck(context.Context) error           { return nil }
func (m *loopSettlementMsg) Nak() error                              { m.naks.Add(1); return nil }
func (m *loopSettlementMsg) NakWithDelay(time.Duration) error        { m.naks.Add(1); return nil }
func (*loopSettlementMsg) InProgress() error                         { return nil }
func (m *loopSettlementMsg) Term() error                             { m.terms.Add(1); return nil }
func (m *loopSettlementMsg) TermWithReason(string) error             { return m.Term() }
