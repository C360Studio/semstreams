//go:build integration

// Integration tests for agentic/agentrun exercising the real lifecycle.Manager
// projection path against a testcontainer NATS server (ADR-053 C2 review finding).
//
// Build tag: go test -tags=integration -race ./agentic/agentrun/...
package agentrun_test

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/agentic/agentrun"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	semerrs "github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type integrationMilestoneHandler struct {
	events chan agentrun.LoopTerminalEvent
}

type blockingIntegrationMilestoneHandler struct {
	entered chan struct{}
	release chan struct{}
	ctxErr  chan error
}

func (h *blockingIntegrationMilestoneHandler) OnLoopTerminal(
	ctx context.Context, _ agentrun.LoopTerminalEvent, _ *agentrun.AgentRun,
) error {
	close(h.entered)
	<-h.release
	h.ctxErr <- ctx.Err()
	return nil
}

func (h *integrationMilestoneHandler) OnLoopTerminal(_ context.Context, event agentrun.LoopTerminalEvent, _ *agentrun.AgentRun) error {
	h.events <- event
	return nil
}

// TestIntegration_D1_ProjectionRoundTrip verifies that lifecycle.Manager.Get
// correctly populates AgentRun.EntityIDField with the FULL 6-part chain execution
// entity ID (ADR-053 D1 critical invariant). The projection layer reads the
// `lifecycle:"id"` field from the ENTITY_STATES KV key — not from triples.
//
// This test drives the production authority-read path:
//  1. Serve a crafted ExactEntity (with the agent.run.phase phase triple) on
//     graph.ingest.query.entity, as graph-ingest does.
//  2. Call Manager.Get (production entry point, uses the exact reader).
//  3. Assert EntityIDField == full 6-part chain.execution entity ID (not a bare UUID,
//     not a garbled form like TryChainExecutionEntityID would reject).
//
// The D1 invariant is specifically about exact authority projection, so this
// test does not add mutation behavior already covered by lifecycle integration.
func TestIntegration_D1_ProjectionRoundTrip(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	mgr := lifecycle.NewManager(tc.Client, nil)
	require.NoError(t, agentrun.Register(mgr), "Register must succeed on a fresh manager")

	// Build the full entity ID that Mint would produce.
	const (
		org        = "acme"
		platform   = "ops"
		rootLoopID = "d1-integration-test-loop"
	)
	fullEntityID := "acme.ops.agent.chain.execution." + rootLoopID

	// Build the exact authority value returned by graph-ingest.
	now := time.Now().UTC()
	state := graph.EntityState{
		ID: fullEntityID,
		Triples: []message.Triple{
			{
				Subject:    fullEntityID,
				Predicate:  agentrun.PhasePredicate, // "agent.run.phase"
				Object:     "dispatched",
				Source:     "test",
				Timestamp:  now,
				Confidence: 1.0,
			},
		},
		Version:   1,
		UpdatedAt: now,
	}
	_, err := tc.Client.SubscribeForRequests(ctx, "graph.ingest.query.entity",
		func(_ context.Context, request []byte) ([]byte, error) {
			var query struct {
				ID string `json:"id"`
			}
			if decodeErr := json.Unmarshal(request, &query); decodeErr != nil {
				return nil, decodeErr
			}
			require.Equal(t, fullEntityID, query.ID)
			return json.Marshal(graph.ExactEntity{Entity: state.Clone(), KVRevision: 1})
		})
	require.NoError(t, err)

	// Manager.Get — the production projection path.
	participant, err := mgr.Get(ctx, agentrun.WorkflowName, fullEntityID)
	require.NoError(t, err, "Manager.Get must succeed when entity state is in ENTITY_STATES")
	require.NotNil(t, participant)

	run, ok := participant.(*agentrun.AgentRun)
	require.True(t, ok, "Manager.Get must return *agentrun.AgentRun for agent-run workflow")

	// D1 critical: EntityIDField must hold the FULL 6-part chain.execution entity ID.
	// The projection layer reads this from the KV key (entityID param), NOT from triples.
	assert.Equal(t, fullEntityID, run.EntityIDField,
		"D1: EntityIDField must hold the full 6-part entity ID populated from the KV key")
	assert.Equal(t, "dispatched", run.PhaseField,
		"PhaseField must be projected from the agent.run.phase triple")
	assert.False(t, run.IsTerminal(), "dispatched must not be terminal")

	// RunID() must derive the bare loop ID from EntityIDField — not a dot-separated path.
	runID, ok := run.RunID()
	require.True(t, ok, "RunID() must succeed for a valid chain.execution entity ID")
	assert.Equal(t, rootLoopID, runID,
		"RunID() must return the bare loop UUID, not the full entity ID")
	assert.NotContains(t, runID, ".",
		"bare RunID must not contain dots — if it does, D1 EntityIDField holds a bare ID, not the full 6-part ID")
}

// TestIntegration_MilestoneSubscriber_GracefulSkipWhenStreamAbsent pins gh#246:
// Start MUST NOT abort boot when the AGENT stream is absent (a deployment with no
// agentic components — e.g. graph/lifecycle-only). It logs and returns a no-op stop
// + nil error instead of the "stream not found" error that previously propagated out
// of run() in both binaries → os.Exit(1) (the silent-red e2e:lifecycle/structural
// tiers and the latent production boot failure).
//
// Since gh#1073 this decision is read off the GUARDED consumer setup rather than
// an unguarded GetStream precondition, so the absence it asserts is now "absent
// continuously for natsclient's stream-visibility budget", not "absent on one
// probe". That is what makes the graceful skip safe on a clustered node that is
// merely lagging, and it is why this test pays one budget in wall clock.
func TestIntegration_MilestoneSubscriber_GracefulSkipWhenStreamAbsent(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	ctx := context.Background()

	mgr := lifecycle.NewManager(tc.Client, nil)
	require.NoError(t, agentrun.Register(mgr))
	// The reader is unused on the stream-absent early-return path; nil logger
	// is defaulted by the constructor.
	sub := agentrun.NewMilestoneSubscriber(mgr, nil, "acme", "ops", nil)

	stop, err := sub.Start(ctx, tc.Client, agentrun.StartConfig{StreamName: agentrun.AgentStreamName})
	require.NoError(t, err, "Start must not error when the AGENT stream is absent (gh#246)")
	require.NotNil(t, stop, "Start must return a non-nil (no-op) stop func when skipping")
	require.NoError(t, stop(ctx))

	// And it did not CREATE the stream to make the error go away. This subscriber is
	// the framework's reference read-only consumer, and a stream's limits belong to
	// the component that declares it: a consumer that reached for get-or-create
	// would either invent limits it does not own or have its own declaration
	// silently discarded, after which the stream's limits are decided by boot order.
	_, getErr := tc.Client.GetStream(ctx, agentrun.AgentStreamName)
	require.ErrorIs(t, getErr, jetstream.ErrStreamNotFound,
		"a read-only consumer must bind by name, never provision the stream it reads")
}

// TestIntegration_MilestoneSubscriberBindsAStreamThatAppearsDuringStart is the
// agentrun half of gh#1073. Start no longer gates on a cheap unguarded
// GetStream, so a stream that is not visible yet — a clustered node that has not
// applied the meta assignment — no longer disables the subscriber for the
// process lifetime behind a successful boot. The stream appears while Start is
// still inside the guarded setup, and the subscriber ends up LIVE.
//
// Liveness is proven on the wire, not from the returned stop: the disabled path
// also returns a non-nil stop, so a stop-shaped assertion would pass for exactly
// the failure this test exists to catch. Synchronization is the production
// wait's own traffic — a second $JS.API.STREAM.INFO.<stream> probe exists only
// because the first was answered "stream not found" — so no delay stands in for
// the proof.
func TestIntegration_MilestoneSubscriberBindsAStreamThatAppearsDuringStart(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	// A deadline, not a delay: nats.Conn.FlushWithContext refuses a
	// deadline-free context. Every wait below ends on its own synchronization
	// long before this expires.
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	_, getErr := tc.GetStream(ctx, agentrun.AgentStreamName)
	require.ErrorIs(t, getErr, jetstream.ErrStreamNotFound,
		"precondition: the AGENT stream must be absent when Start begins")

	conn := tc.GetNativeConnection()
	probes, err := conn.SubscribeSync("$JS.API.STREAM.INFO." + agentrun.AgentStreamName)
	require.NoError(t, err)
	t.Cleanup(func() { _ = probes.Unsubscribe() })
	require.NoError(t, conn.FlushWithContext(ctx))

	created := make(chan error, 1)
	go func() {
		probeCtx, probeCancel := context.WithTimeout(ctx, 2*time.Second)
		defer probeCancel()
		for range 2 {
			if _, waitErr := probes.NextMsgWithContext(probeCtx); waitErr != nil {
				created <- fmt.Errorf("Start stopped probing for the absent stream: %w", waitErr)
				return
			}
		}
		_, createErr := tc.CreateStream(ctx, agentrun.AgentStreamName, []string{"agent.>"})
		created <- createErr
	}()

	mgr := lifecycle.NewManager(tc.Client, nil)
	require.NoError(t, agentrun.Register(mgr))
	sub := agentrun.NewMilestoneSubscriber(mgr, nil, "acme", "ops", nil)
	handler := &integrationMilestoneHandler{events: make(chan agentrun.LoopTerminalEvent, 1)}
	sub.AddHandler(handler)

	stop, err := sub.Start(ctx, tc.Client, agentrun.StartConfig{
		StreamName: agentrun.AgentStreamName, ConsumerNameSuffix: "late-visible",
	})
	require.NoError(t, <-created)
	require.NoError(t, err, "Start must bind a stream that becomes visible while it is waiting")
	require.NotNil(t, stop)
	defer func() { require.NoError(t, stop(ctx)) }()

	payload := &agentic.LoopCompletedEvent{
		LoopID: "run-late-visible", TaskID: "task-late-visible", Outcome: agentic.OutcomeSuccess,
		CompletedAt: time.Now().UTC(), RunEntityID: "missing-run-late-visible",
	}
	data, marshalErr := json.Marshal(message.NewBaseMessage(payload.Schema(), payload, "agentic-loop"))
	require.NoError(t, marshalErr)
	require.NoError(t, tc.Client.PublishToStream(ctx, "agent.complete."+payload.Schema().Category, data))

	select {
	case event := <-handler.events:
		assert.Equal(t, "run-late-visible", event.LoopID)
		assert.Equal(t, agentic.CategoryLoopCompleted, event.Category)
	case <-ctx.Done():
		t.Fatal("a subscriber that bound a late-appearing stream did not handle its milestone")
	}
}

// TestIntegration_MilestoneSubscriberCancelledBootIsNotAGracefulSkip pins the
// CONSTRUCTION the disabled-when-absent branch relies on: a wait the caller ended
// never carries natsclient.ErrStreamNotVisible, so the branch's single condition
// fails closed on a cancelled boot without ordering anything.
//
// The error it does carry still has jetstream.ErrStreamNotFound reachable
// alongside the caller's cause, which is exactly why absence is not decided from
// that classification: doing so would report "no agentic components in this
// deployment" for a boot that was merely cancelled — a positive claim about a
// fact never established. If the two endings ever converge, this test fails.
func TestIntegration_MilestoneSubscriberCancelledBootIsNotAGracefulSkip(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	// The AGENT stream is absent, so Start enters the visibility wait; this
	// deadline ends that wait before the framework's budget does, which is what a
	// boot cancelled mid-wait looks like from inside Start.
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	mgr := lifecycle.NewManager(tc.Client, nil)
	require.NoError(t, agentrun.Register(mgr))
	sub := agentrun.NewMilestoneSubscriber(mgr, nil, "acme", "ops", nil)

	stop, err := sub.Start(ctx, tc.Client, agentrun.StartConfig{
		StreamName: agentrun.AgentStreamName, ConsumerNameSuffix: "cancelled-boot",
	})
	require.Error(t, err, "a cancelled boot must not be reported as a graceful skip")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorIs(t, err, jetstream.ErrStreamNotFound,
		"both causes stay reachable, which is why the branch reads the sentinel instead")
	require.NotErrorIs(t, err, natsclient.ErrStreamNotVisible,
		"a wait the caller ended measured nothing, so it carries no evidence of absence")
	require.Nil(t, stop)
}

// TestIntegration_MilestoneSubscriber_StartsWhenStreamPresent confirms the normal
// path is unchanged by the gh#246 graceful-skip: with the AGENT stream present,
// Start wires the durable consumers and the subscriber is LIVE.
//
// Liveness is asserted on the wire, by a handled milestone. A nil error plus a
// non-nil stop is NOT the assertion to make here: the graceful-skip path returns
// exactly that shape, so this test passed while the subscriber had silently
// disabled itself — which is how a not-found from consumer CREATION, with the
// stream present, went unnoticed.
func TestIntegration_MilestoneSubscriber_StartsWhenStreamPresent(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	ctx := t.Context()

	_, err := tc.CreateStream(ctx, agentrun.AgentStreamName, []string{"agent.>"})
	require.NoError(t, err, "create AGENT stream")

	mgr := lifecycle.NewManager(tc.Client, nil)
	require.NoError(t, agentrun.Register(mgr))
	sub := agentrun.NewMilestoneSubscriber(mgr, nil, "acme", "ops", nil)
	handler := &integrationMilestoneHandler{events: make(chan agentrun.LoopTerminalEvent, 1)}
	sub.AddHandler(handler)

	stop, err := sub.Start(ctx, tc.Client, agentrun.StartConfig{
		StreamName:         agentrun.AgentStreamName,
		ConsumerNameSuffix: "gh246-present",
	})
	require.NoError(t, err, "Start must succeed when the AGENT stream is present")
	require.NotNil(t, stop)
	defer func() { require.NoError(t, stop(ctx)) }()

	payload := &agentic.LoopCompletedEvent{
		LoopID: "run-present", TaskID: "task-present", Outcome: agentic.OutcomeSuccess,
		CompletedAt: time.Now().UTC(), RunEntityID: "missing-run-present",
	}
	data, marshalErr := json.Marshal(message.NewBaseMessage(payload.Schema(), payload, "agentic-loop"))
	require.NoError(t, marshalErr)
	require.NoError(t, tc.Client.PublishToStream(ctx, "agent.complete."+payload.Schema().Category, data))

	select {
	case event := <-handler.events:
		assert.Equal(t, "run-present", event.LoopID)
	case <-time.After(10 * time.Second):
		t.Fatal("a subscriber reported as started did not handle a milestone")
	}
}

func TestIntegration_MilestoneSubscriberDrainsBothHandlesBeforeWaiting(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: agentrun.AgentStreamName, Subjects: []string{"agent.>"}},
	))
	defer tc.Terminate()
	mgr := lifecycle.NewManager(tc.Client, nil)
	require.NoError(t, agentrun.Register(mgr))
	sub := agentrun.NewMilestoneSubscriber(mgr, nil, "acme", "ops", nil)
	handler := &blockingIntegrationMilestoneHandler{
		entered: make(chan struct{}), release: make(chan struct{}), ctxErr: make(chan error, 1),
	}
	sub.AddHandler(handler)
	stop, err := sub.Start(t.Context(), tc.Client, agentrun.StartConfig{
		StreamName: agentrun.AgentStreamName, ConsumerNameSuffix: "drain-order",
	})
	require.NoError(t, err)

	payload := &agentic.LoopCompletedEvent{
		LoopID: "blocked-run", TaskID: "blocked-task", Outcome: agentic.OutcomeSuccess,
		CompletedAt: time.Now().UTC(), RunEntityID: "missing-blocked-run",
	}
	data, err := json.Marshal(message.NewBaseMessage(payload.Schema(), payload, "agentic-loop"))
	require.NoError(t, err)
	require.NoError(t, tc.Client.PublishToStream(
		t.Context(), "agent.complete."+payload.Schema().Category, data,
	))
	<-handler.entered

	stopResult := make(chan error, 1)
	go func() { stopResult <- stop(t.Context()) }()
	type acquireResult struct {
		handle jetstream.ConsumeContext
		err    error
	}
	reacquired := make(chan acquireResult, 1)
	go func() {
		cfg := natsclient.StreamConsumerConfig{
			StreamName: agentrun.AgentStreamName, ConsumerName: "agentrun-milestone-failed-drain-order",
			FilterSubject: "agent.failed.*", AckPolicy: "explicit", DeliverPolicy: "new",
			MaxDeliver: 5, AckWait: 30 * time.Second,
		}
		for {
			handle, acquireErr := tc.Client.ConsumeInternalStreamWithConfig(
				t.Context(), cfg, func(context.Context, jetstream.Msg) {},
			)
			if acquireErr == nil || !semerrs.IsInvalid(acquireErr) {
				reacquired <- acquireResult{handle: handle, err: acquireErr}
				return
			}
			runtime.Gosched()
		}
	}()
	acquired := <-reacquired
	require.NoError(t, acquired.err,
		"failed handle must close even while the complete callback keeps Stop waiting")
	select {
	case stopErr := <-stopResult:
		t.Fatalf("Stop returned before the admitted complete callback: %v", stopErr)
	default:
	}
	close(handler.release)
	require.NoError(t, <-handler.ctxErr, "callback authority must remain live through native Closed")
	require.NoError(t, <-stopResult)
	acquired.handle.Drain()
	<-acquired.handle.Closed()
}

func TestIntegration_MilestoneSubscriberSecondFailureRollsBackFirstHandle(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: agentrun.AgentStreamName, Subjects: []string{"agent.>"}},
	))
	defer tc.Terminate()
	failedCfg := natsclient.StreamConsumerConfig{
		StreamName: agentrun.AgentStreamName, ConsumerName: "agentrun-milestone-failed-partial",
		FilterSubject: "agent.failed.*", AckPolicy: "explicit", DeliverPolicy: "new",
	}
	incumbent, err := tc.Client.ConsumeInternalStreamWithConfig(
		t.Context(), failedCfg, func(context.Context, jetstream.Msg) {},
	)
	require.NoError(t, err)
	mgr := lifecycle.NewManager(tc.Client, nil)
	require.NoError(t, agentrun.Register(mgr))
	sub := agentrun.NewMilestoneSubscriber(mgr, nil, "acme", "ops", nil)
	cleanup, startErr := sub.Start(t.Context(), tc.Client, agentrun.StartConfig{
		StreamName: agentrun.AgentStreamName, ConsumerNameSuffix: "partial",
	})
	require.Error(t, startErr)
	require.Nil(t, cleanup, "successful bounded rollback clears partial cleanup authority")

	completeCfg := failedCfg
	completeCfg.ConsumerName = "agentrun-milestone-complete-partial"
	completeCfg.FilterSubject = "agent.complete.*"
	var complete jetstream.ConsumeContext
	for {
		complete, err = tc.Client.ConsumeInternalStreamWithConfig(
			t.Context(), completeCfg, func(context.Context, jetstream.Msg) {},
		)
		if err == nil {
			break
		}
		require.True(t, semerrs.IsInvalid(err), "unexpected reacquisition failure: %v", err)
		runtime.Gosched()
	}
	complete.Drain()
	<-complete.Closed()
	incumbent.Drain()
	<-incumbent.Closed()
}

func TestIntegration_MilestoneSubscriberProductionEnvelopeCallbacks(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: agentrun.AgentStreamName, Subjects: []string{"agent.>"}},
	))
	ctx := t.Context()
	mgr := lifecycle.NewManager(tc.Client, nil)
	require.NoError(t, agentrun.Register(mgr))
	sub := agentrun.NewMilestoneSubscriber(mgr, nil, "acme", "ops", nil)
	handler := &integrationMilestoneHandler{events: make(chan agentrun.LoopTerminalEvent, 3)}
	sub.AddHandler(handler)
	stop, err := sub.Start(ctx, tc.Client, agentrun.StartConfig{StreamName: agentrun.AgentStreamName, ConsumerNameSuffix: "terminal-production"})
	require.NoError(t, err)
	defer func() { require.NoError(t, stop(ctx)) }()

	at := time.Now().UTC()
	payloads := []message.Payload{
		&agentic.LoopCompletedEvent{LoopID: "run-success", TaskID: "task-success", Outcome: agentic.OutcomeSuccess, CompletedAt: at, RunEntityID: "missing-run-success"},
		&agentic.LoopFailedEvent{LoopID: "run-failed", TaskID: "task-failed", Outcome: agentic.OutcomeFailed, FailedAt: at, RunEntityID: "missing-run-failed"},
		&agentic.LoopCancelledEvent{LoopID: "run-cancelled", TaskID: "task-cancelled", Outcome: agentic.OutcomeCancelled, CancelledAt: at, RunEntityID: "missing-run-cancelled"},
	}
	for _, payload := range payloads {
		data, marshalErr := json.Marshal(message.NewBaseMessage(payload.Schema(), payload, "agentic-loop"))
		require.NoError(t, marshalErr)
		subjectPrefix := "agent.complete."
		if payload.Schema().Category == agentic.CategoryLoopFailed {
			subjectPrefix = "agent.failed."
		}
		require.NoError(t, tc.Client.PublishToStream(ctx, subjectPrefix+payload.Schema().Category, data))
	}

	want := map[string]bool{
		agentic.CategoryLoopCompleted: false,
		agentic.CategoryLoopFailed:    false,
		agentic.CategoryLoopCancelled: false,
	}
	for range 3 {
		select {
		case event := <-handler.events:
			want[event.Category] = true
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for AgentRun production-envelope callback")
		}
	}
	for category, observed := range want {
		require.True(t, observed, category)
	}
}
