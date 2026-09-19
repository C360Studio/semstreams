package agenticdispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/pkg/graphview"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dispatchSettlementMsg struct {
	data  []byte
	acks  atomic.Int32
	naks  atomic.Int32
	terms atomic.Int32
}

// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestDispatchProductionCallbacksDoNotAckFalseDone(t *testing.T) {
	t.Run("unknown task publication quarantines", func(t *testing.T) {
		deps := componentDependenciesForCausalTest()
		deps.PayloadRegistry = payloadbuiltins.NewTestRegistry(t)
		discoverable, err := NewComponent([]byte(`{}`), deps)
		require.NoError(t, err)
		c := discoverable.(*Component)
		c.modelRegistry = newTestRegistry()
		c.taskEvidence = emptyRetainedTaskEvidenceReader{}
		withPersistedLoops(c, nil)
		c.waitForStreamInput = func(context.Context, string) error { return nil }
		callbacks := make(map[string]func(context.Context, jetstream.Msg))
		handles := make(map[string]*causalConsumeHandle)
		c.consumeStream = func(_ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			handle := &causalConsumeHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 1)}
			callbacks[owner.Port] = callback
			handles[owner.Port] = handle
			return handle, nil
		}
		ctx, cancel := context.WithCancel(t.Context())
		require.NoError(t, c.setupSubscriptions(ctx))

		msg := &dispatchSettlementMsg{data: mustMarshalDispatchSettlementPayload(t, &agentic.UserMessage{
			MessageID: "message-failed-publish", ChannelType: "cli", ChannelID: "channel-1", UserID: "user-1",
			Content: "do the work", Timestamp: time.Now().UTC(),
		})}
		callbacks["user.message"](ctx, msg)
		require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())
		require.Eventually(t, func() bool { return handles["user.message"].drains.Load() == 1 }, time.Second, time.Millisecond)
		for port, handle := range handles {
			if port != "user.message" {
				require.Zero(t, handle.drains.Load(), "task failure drained unrelated owner %s", port)
			}
		}
		require.Contains(t, c.Health().LastError, "unknown durable state")
		cancel()
		for _, binding := range c.consumers {
			<-binding.observerDone
		}
	})

	// The response-PubAck gate (component.go:1284 -> Ack :910) had no observer:
	// the sendResponseFn seam short-circuits sendResponse before PublishToStream
	// (:1270-1273), so a test using it cannot see the publish at all. The
	// unknown-command path reaches the production sendResponse with the user
	// response as its ONLY required publication, which isolates that gate: if
	// the publish fails and the callback still Acks, the user was told nothing
	// and the input is gone.
	t.Run("failed user-response publication retries, never acks", func(t *testing.T) {
		deps := componentDependenciesForCausalTest()
		deps.PayloadRegistry = payloadbuiltins.NewTestRegistry(t)
		discoverable, err := NewComponent([]byte(`{}`), deps)
		require.NoError(t, err)
		c := discoverable.(*Component)
		c.modelRegistry = newTestRegistry()
		require.Nil(t, c.sendResponseFn,
			"this case must run the production sendResponse; the test seam would skip the publish it exists to observe")
		c.waitForStreamInput = func(context.Context, string) error { return nil }
		callbacks := make(map[string]func(context.Context, jetstream.Msg))
		handles := make(map[string]*causalConsumeHandle)
		c.consumeStream = func(_ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			handle := &causalConsumeHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 1)}
			callbacks[owner.Port] = callback
			handles[owner.Port] = handle
			return handle, nil
		}
		ctx, cancel := context.WithCancel(t.Context())
		require.NoError(t, c.setupSubscriptions(ctx))

		// An unrecognised command: handleCommand answers it with a typed error
		// response (component.go:916-926) and publishes nothing else, so the
		// failing publish below is the user response and only the user response.
		msg := &dispatchSettlementMsg{data: mustMarshalDispatchSettlementPayload(t, &agentic.UserMessage{
			MessageID: "message-response-publish-fails", ChannelType: "cli", ChannelID: "channel-1", UserID: "user-1",
			Content: "/not-a-command", Timestamp: time.Now().UTC(),
		})}
		callbacks["user.message"](ctx, msg)

		require.Zero(t, msg.acks.Load(),
			"a user response that did not reach the stream must not settle its source as done")
		require.Equal(t, int32(1), msg.naks.Load(),
			"a failed required publication is transient: Retry, per the matrix that reserves Quarantine for unknown durable state")
		require.Zero(t, msg.terms.Load())
		require.NotContains(t, c.Health().LastError, "unknown durable state",
			"a response publish failure is not an owner-fatal latch")
		for port, handle := range handles {
			require.Zero(t, handle.drains.Load(), "a retryable publish failure must not drain owner %s", port)
		}
		cancel()
		for _, binding := range c.consumers {
			<-binding.observerDone
		}
	})
}

func (m *dispatchSettlementMsg) Data() []byte                            { return m.data }
func (*dispatchSettlementMsg) Subject() string                           { return "dispatch.test" }
func (*dispatchSettlementMsg) Reply() string                             { return "" }
func (*dispatchSettlementMsg) Headers() nats.Header                      { return nil }
func (*dispatchSettlementMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (m *dispatchSettlementMsg) Ack() error                              { m.acks.Add(1); return nil }
func (*dispatchSettlementMsg) DoubleAck(context.Context) error           { return nil }
func (m *dispatchSettlementMsg) Nak() error                              { m.naks.Add(1); return nil }
func (m *dispatchSettlementMsg) NakWithDelay(time.Duration) error        { m.naks.Add(1); return nil }
func (*dispatchSettlementMsg) InProgress() error                         { return nil }
func (m *dispatchSettlementMsg) Term() error                             { m.terms.Add(1); return nil }
func (m *dispatchSettlementMsg) TermWithReason(string) error             { return m.Term() }

func TestTerminalDeliveryFatalBuffersBeforeHandleAndDrainsExactHandleOnce(t *testing.T) {
	result := natsclient.ConsumeDeliveryWithHeartbeat(t.Context(), nil, natsclient.HeartbeatDeliveryPolicy{})
	require.True(t, result.OwnerStopRequired())
	observed := make(chan error, 1)
	c := &Component{
		started:                true,
		logger:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
		terminalDeliveryDoneFn: func(err error) { observed <- err },
	}
	admission := newDeliveryLaneAdmission(c.recordAgentCompleteFatal, nil)
	admission.latch(result)
	require.Len(t, admission.fatal, 1)
	health := c.Health()
	require.False(t, health.Healthy)
	require.Contains(t, health.LastError, "agent.complete")

	handle := &causalConsumeHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 1)}
	binding := newStreamConsumerBinding(handle)
	ctx, cancel := context.WithCancel(t.Context())
	c.observeDeliveryLane(ctx, &binding, admission)
	select {
	case err := <-observed:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("fatal result was not observed")
	}
	require.Eventually(t, func() bool { return handle.drains.Load() == 1 }, time.Second, time.Millisecond)
	binding.drain()
	require.Equal(t, int32(1), handle.drains.Load())
	require.False(t, admission.admit())
	cancel()
	<-binding.observerDone
}

// The two terminal lanes keep their own fatal fields, so a fatal on one does
// not erase which lane failed.
func TestTerminalLaneFatalHealthFailsClosedIndependently(t *testing.T) {
	result := natsclient.ConsumeDeliveryWithHeartbeat(t.Context(), nil, natsclient.HeartbeatDeliveryPolicy{})
	tests := []struct {
		name   string
		lane   string
		record func(*Component, natsclient.DeliveryResult)
	}{
		{name: "complete", lane: "agent.complete", record: (*Component).recordAgentCompleteFatal},
		{name: "failed", lane: "agent.failed", record: (*Component).recordAgentFailedFatal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Component{started: true}
			admission := newDeliveryLaneAdmission(func(result natsclient.DeliveryResult) { tt.record(c, result) }, nil)
			admission.latch(result)
			health := c.Health()
			require.False(t, health.Healthy)
			require.Equal(t, "terminal delivery ownership lost", health.Status)
			require.Equal(t, 1, health.ErrorCount)
			require.Contains(t, health.LastError, tt.lane)
		})
	}
}

// The three lanes this change brings under settlement share one latch, and it
// keeps the FIRST cause: a later fatal neither overwrites nor recounts it.
func TestDeliveryFatalHealthKeepsFirstCauseAcrossLanes(t *testing.T) {
	result := natsclient.ConsumeDeliveryWithHeartbeat(t.Context(), nil, natsclient.HeartbeatDeliveryPolicy{})
	c := &Component{started: true}
	newDeliveryLaneAdmission(c.recordDeliveryOwnerFatal, nil).latch(result)
	first := c.Health()
	newDeliveryLaneAdmission(c.recordDeliveryOwnerFatal, nil).latch(result)
	second := c.Health()
	require.False(t, first.Healthy)
	require.Equal(t, "delivery ownership lost", first.Status)
	require.Equal(t, 1, first.ErrorCount)
	require.Equal(t, first.LastError, second.LastError)
	require.Equal(t, first.ErrorCount, second.ErrorCount)
}

// A refused delivery is a declared skip, not a silent drop: every call-site
// branch is guarded on admission, so the refusal is invisible unless the lane
// names it. The lanes drain rather than stop, so buffered deliveries do
// reach this path.
func TestRefusedTerminalDeliveryIsLoggedAndCounted(t *testing.T) {
	logs := &bytes.Buffer{}
	c := &Component{
		started: true,
		logger:  slog.New(slog.NewTextHandler(logs, nil)),
		metrics: getMetrics(metric.NewMetricsRegistry()),
	}
	before := testutil.ToFloat64(c.metrics.deliveryRefusals.WithLabelValues("agent.complete"))
	admission := newDeliveryLaneAdmission(c.recordAgentCompleteFatal, func(subject string) {
		c.recordDeliveryRefused("agent.complete", subject)
	})
	fatal := natsclient.ConsumeDeliveryWithHeartbeat(t.Context(), nil, natsclient.HeartbeatDeliveryPolicy{})
	admission.latch(fatal)

	result, admitted := consumeAdmittedDelivery(t.Context(), &refusedDeliveryMsg{}, natsclient.HeartbeatDeliveryPolicy{}, admission)

	require.False(t, admitted)
	require.Equal(t, natsclient.DeliveryResult{}, result)
	require.Equal(t, before+1,
		testutil.ToFloat64(c.metrics.deliveryRefusals.WithLabelValues("agent.complete")),
		"a refused delivery must increment the refusal counter")
	require.Contains(t, logs.String(), "Terminal delivery refused by latched lane")
	require.Contains(t, logs.String(), "subject=agent.complete")
}

type refusedDeliveryMsg struct{ jetstream.Msg }

func (*refusedDeliveryMsg) Subject() string { return "agent.complete" }

// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestDispatchProductionCallbacksTerminateMalformedNonHeartbeatInputs(t *testing.T) {
	deps := componentDependenciesForCausalTest()
	deps.PayloadRegistry = payloadbuiltins.NewTestRegistry(t)
	discoverable, err := NewComponent([]byte(`{}`), deps)
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	responses := make([]agentic.UserResponse, 0, 1)
	c.sendResponseFn = func(response agentic.UserResponse) { responses = append(responses, response) }
	callbacks := make(map[string]func(context.Context, jetstream.Msg))
	c.consumeStream = func(_ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		callbacks[owner.Port] = callback
		return &causalConsumeHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 1)}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, c.setupSubscriptions(ctx))

	require.NotContains(t, callbacks, "agent.created")
	require.NotContains(t, callbacks, "agent.approval_pending")
	for _, port := range []string{"user.message"} {
		callback, ok := callbacks[port]
		require.True(t, ok, "production setup did not bind %s", port)
		msg := &dispatchSettlementMsg{data: []byte("{")}
		callback(ctx, msg)
		require.Zero(t, msg.acks.Load(), "%s must not ACK malformed input", port)
		require.Zero(t, msg.naks.Load(), "%s immutable malformed input must not retry", port)
		require.Equal(t, int32(1), msg.terms.Load(), "%s immutable malformed input must terminate", port)
	}

	valid := map[string][]byte{
		"user.message": mustMarshalDispatchSettlementPayload(t, &agentic.UserMessage{
			MessageID: "message-1", ChannelType: "cli", ChannelID: "channel-1", UserID: "user-1", Content: "/help", Timestamp: time.Now().UTC(),
		}),
	}
	for _, port := range []string{"user.message"} {
		msg := &dispatchSettlementMsg{data: valid[port]}
		callbacks[port](ctx, msg)
		require.Equal(t, int32(1), msg.acks.Load(), "%s successful declared consequence must ACK", port)
		require.Zero(t, msg.naks.Load()+msg.terms.Load())
	}
	require.Len(t, responses, 1)
	require.Contains(t, responses[0].Content, "/help")

	cancel()
	for _, binding := range c.consumers {
		if binding.observerDone != nil {
			<-binding.observerDone
		}
	}
}

func mustMarshalDispatchSettlementPayload(t *testing.T, payload message.Payload) []byte {
	t.Helper()
	encoded, err := json.Marshal(message.NewBaseMessage(payload.Schema(), payload, "test"))
	require.NoError(t, err)
	return encoded
}

var _ component.Discoverable = (*Component)(nil)

// R1 quarantined a post-effect response failure on the command lane, and round
// 6 found the predicate too wide: it keyed on where the TARGET came from, which
// is true of every argument-less command while auto-continue is on — `/help`,
// `/loops`, a bare `/status` — and of the three arms of bare `/cancel` that
// publish nothing. None of them can be un-done by a replay, because none of
// them did anything; quarantining them latches the whole user.message lane on a
// failed response to a read-only command, and the cause text names a loop they
// never touched.
//
// The predicate is now two conjuncts, and this test holds the half that must
// NOT quarantine. The published fact comes from the publish site itself
// (commands.go:193), so these cases are distinguished by what they did rather
// than by what they were called.
//
// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestEffectFreeCommandWithFailedResponseRetries(t *testing.T) {
	const (
		activeLoopID  = "00000000-0000-4000-8000-0000000000a1"
		settledLoopID = "00000000-0000-4000-8000-0000000000a2"
	)
	// The production callback, with the production sendResponse: the client is
	// constructed and never connected, so the user response is the publication
	// that fails.
	newLane := func(t *testing.T) (*Component, func(context.Context, jetstream.Msg), map[string]*causalConsumeHandle, context.Context, context.CancelFunc) {
		t.Helper()
		deps := componentDependenciesForCausalTest()
		deps.PayloadRegistry = payloadbuiltins.NewTestRegistry(t)
		discoverable, err := NewComponent([]byte(`{}`), deps)
		require.NoError(t, err)
		c := discoverable.(*Component)
		c.modelRegistry = newTestRegistry()
		require.Nil(t, c.sendResponseFn,
			"this case must run the production sendResponse; the seam would skip the publish it exists to observe")
		// resolveConfig defaults DefaultRole, StreamName and Permissions but NOT
		// AutoContinue, so a component built from `{}` has it false while
		// DefaultConfig(), the struct tag and the published schema all say true
		// (#1348: resolveConfig does not apply the advertised default). The
		// hazard this test covers needs the tracker branch reachable, which is
		// what the flag turns on — so it is set here deliberately rather than
		// inherited.
		c.config.AutoContinue = true
		c.waitForStreamInput = func(context.Context, string) error { return nil }
		callbacks := make(map[string]func(context.Context, jetstream.Msg))
		handles := make(map[string]*causalConsumeHandle)
		c.consumeStream = func(_ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			handle := &causalConsumeHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 1)}
			callbacks[owner.Port] = callback
			handles[owner.Port] = handle
			return handle, nil
		}
		ctx, cancel := context.WithCancel(t.Context())
		require.NoError(t, c.setupSubscriptions(ctx))
		return c, callbacks["user.message"], handles, ctx, cancel
	}
	currentLoop := func(loopID string) *agentic.LoopEntity {
		return &agentic.LoopEntity{
			ID: loopID, TaskID: "task-" + loopID, UserID: "user-1",
			ChannelType: "cli", ChannelID: "channel-1",
			State: agentic.LoopStateExecuting, MaxIterations: 5,
		}
	}
	command := func(t *testing.T, id, content string) *dispatchSettlementMsg {
		t.Helper()
		return &dispatchSettlementMsg{data: mustMarshalDispatchSettlementPayload(t, &agentic.UserMessage{
			MessageID: id, ChannelType: "cli", ChannelID: "channel-1", UserID: "user-1",
			Content: content, Timestamp: time.Now().UTC(),
		})}
	}
	requireRetriedWithLaneIntact := func(t *testing.T, c *Component, msg *dispatchSettlementMsg, handles map[string]*causalConsumeHandle) {
		t.Helper()
		// assert, not require: when the decision flips, the three consequence
		// assertions below are the ones that say what it COSTS — the latched
		// fatal and the drained owner — and a require here would abort before
		// any of them ran.
		assert.Equal(t, int32(1), msg.naks.Load(),
			"a command that published nothing is replayable: its failed response is an ordinary Retry")
		assert.Zero(t, msg.acks.Load()+msg.terms.Load())
		// Health().Healthy also requires c.started, which this harness does not
		// set (it binds the production callbacks without Start), so the latch
		// itself is the assertion: deliveryFatalErr is what a Quarantine here
		// would set, and it surfaces as LastError plus the drained handle.
		require.Empty(t, c.Health().LastError,
			"a read-only command's failed response must not latch a delivery-ownership fatal")
		for port, handle := range handles {
			require.Zero(t, handle.drains.Load(), "an effect-free failure must not drain owner %s", port)
		}
	}

	t.Run("a read-only command whose target was resolved, not named", func(t *testing.T) {
		c, deliver, handles, ctx, cancel := newLane(t)
		defer cancel()
		// A current loop on this route, so handleCommand's auto-continue branch
		// resolves a target for the argument-less /help — the exact condition
		// that used to be sufficient to quarantine. The source moved from the
		// process-local tracker to durable authority (#1329); the provenance
		// fact the quarantine arm reads did not.
		seedCurrentLoops(t, c, currentLoop(activeLoopID))
		resolved, err := c.activeLoop(ctx, agentic.UserMessage{
			UserID: "user-1", ChannelType: "cli", ChannelID: "channel-1"})
		require.NoError(t, err)
		require.Equal(t, activeLoopID, resolved)

		msg := command(t, "message-help", "/help")
		deliver(ctx, msg)

		requireRetriedWithLaneIntact(t, c, msg, handles)

		// And the lane still takes work, which is the cost a Quarantine here
		// would have imposed on every later user message.
		second := command(t, "message-help-2", "/help")
		deliver(ctx, second)
		require.Equal(t, int32(1), second.naks.Load(), "the lane refused a later delivery")
	})

	t.Run("a bare cancel whose loop has already settled", func(t *testing.T) {
		c, deliver, handles, ctx, cancel := newLane(t)
		defer cancel()
		// Current in the shared projection, settled in the exact record: the
		// gate reads the record and answers "already settled", so
		// handleCancelCommand returns BEFORE the publish at commands.go:187.
		// The two reads are the view and the exact Get — under #1329 that skew
		// is the projection lagging its own bucket, not a second source of
		// truth, and it is what lets this case resolve a target and still
		// publish nothing.
		seedCurrentLoops(t, c, currentLoop(settledLoopID))
		resolved, err := c.activeLoop(ctx, agentic.UserMessage{
			UserID: "user-1", ChannelType: "cli", ChannelID: "channel-1"})
		require.NoError(t, err)
		require.Equal(t, settledLoopID, resolved,
			"the target must be resolved rather than named, or this case cannot discriminate")
		withPersistedLoops(c, map[string]*agentic.LoopEntity{settledLoopID: {
			ID: settledLoopID, UserID: "user-1", ChannelType: "cli", ChannelID: "channel-1",
			State: agentic.LoopStateComplete, MaxIterations: 5,
		}})

		msg := command(t, "message-bare-cancel-settled", "/cancel")
		deliver(ctx, msg)

		requireRetriedWithLaneIntact(t, c, msg, handles)
	})

	// This one discriminates neither predicate: with no loop to resolve,
	// targetResolved is false, so the old provenance-only arm retried it too.
	// It pins the no-loop path against a future widening, and is not coverage of
	// the two-conjunct rule — the two subtests above are.
	t.Run("a bare cancel with no loop to resolve", func(t *testing.T) {
		c, deliver, handles, ctx, cancel := newLane(t)
		defer cancel()
		seedCurrentLoops(t, c)
		withPersistedLoops(c, nil)

		msg := command(t, "message-bare-cancel-none", "/cancel")
		deliver(ctx, msg)

		requireRetriedWithLaneIntact(t, c, msg, handles)
	})
}

// seedCurrentLoops attaches the shared activity view to a component built for
// the production callbacks and seeds it with current loop records. Under #1329
// this is where an argument-less command's target comes from: handleCommand
// resolves it through activeLoop, which reads this projection, so a lane test
// that needs a resolvable target supplies one here rather than in a
// process-local tracker. It seeds the view only — the exact-read seam is the
// caller's, because the cases that matter here are the ones where the two
// disagree.
func seedCurrentLoops(t *testing.T, c *Component, records ...*agentic.LoopEntity) {
	t.Helper()
	source := newFakeActivitySource()
	c.activityViewSource = source
	c.activityViewOpts = []graphview.Option{graphview.WithTickInterval(2 * time.Millisecond)}
	activityCtx, cancel := context.WithCancel(t.Context())
	c.activityCommands = make(chan activityViewCommand)
	c.activityDone = make(chan struct{})
	c.activityCancel = cancel
	go c.runActivityViewControl(activityCtx, c.activityCommands, c.activityDone)
	t.Cleanup(c.stopActivityView)

	ctx, cancelWait := context.WithTimeout(t.Context(), activityTestWait)
	defer cancelWait()
	view, err := c.ensureActivityView(ctx)
	require.NoError(t, err)
	watcher := source.waitWatcher(t, 1)
	for i, record := range records {
		require.NoError(t, record.Validate())
		data, err := json.Marshal(record)
		require.NoError(t, err)
		watcher.updates <- putEntry(record.ID, data, uint64(i+1))
	}
	watcher.updates <- nil
	require.NoError(t, view.WaitCaughtUp(ctx))
}
