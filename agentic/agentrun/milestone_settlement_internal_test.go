package agentrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/deliverylane"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	semerrs "github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/lifecycle"
)

// These tests drive the exact closure Start hands to the lane: the same
// DeliveryWork, through the same validated HeartbeatDeliveryPolicy, under the
// same Admission, settling a real jetstream.Msg. The settlement METHOD the
// message observes is the assertion — "the decision was Retry" is only true if
// the delivery was actually Naked.

// --- lane fixture -----------------------------------------------------------

// milestoneLane assembles one lane the way Start does. The consumer config is
// the production one: MaxDeliver 5 and AckWait 30s, which is also what makes
// the 10s heartbeat admissible.
type milestoneLaneFixture struct {
	policy    natsclient.HeartbeatDeliveryPolicy
	admission *deliverylane.Admission
}

func newMilestoneLaneFixture(t *testing.T, s *MilestoneSubscriber, lane string) milestoneLaneFixture {
	t.Helper()
	return milestoneLaneFixture{
		policy:    milestonePolicyFor(t, s, lane),
		admission: s.newLaneAdmission(),
	}
}

// milestonePolicyFor validates one lane's policy against the production
// consumer config: MaxDeliver 5 and AckWait 30s, which is also what makes the
// 10s heartbeat admissible.
func milestonePolicyFor(t *testing.T, s *MilestoneSubscriber, lane string) natsclient.HeartbeatDeliveryPolicy {
	t.Helper()
	retry, err := natsclient.DelayedDeliveryRetry(milestoneRetryDelay)
	require.NoError(t, err)
	cfg := natsclient.StreamConsumerConfig{
		StreamName:    AgentStreamName,
		ConsumerName:  "agentrun-milestone-" + lane,
		FilterSubject: "agent." + lane + ".*",
		AckPolicy:     "explicit",
		DeliverPolicy: "new",
		MaxDeliver:    5,
		AckWait:       30 * time.Second,
	}
	policy, err := natsclient.ValidateHeartbeatDeliveryPolicy(
		t.Context(), cfg, milestoneHeartbeatInterval, retry, s.deliveryWork(lane),
	)
	require.NoError(t, err)
	return policy
}

// deliver runs one delivery of data through the lane and returns the message so
// the test can read which terminal method it observed.
func (f milestoneLaneFixture) deliver(t *testing.T, data []byte) *settlementMsg {
	t.Helper()
	msg := &settlementMsg{data: data, subject: "agent.complete.loop_completed", delivered: 1}
	_, admitted := deliverylane.Consume(t.Context(), msg, f.policy, f.admission)
	require.True(t, admitted, "an open lane must admit the delivery")
	return msg
}

// settlementMsg is a jetstream.Msg that records which terminal method the
// settlement contract applied, and with what delay.
type settlementMsg struct {
	data      []byte
	subject   string
	delivered uint64
	metaErr   error

	acks      atomic.Int32
	naks      atomic.Int32
	nakDelays []time.Duration
	terms     atomic.Int32
	heartbeat atomic.Int32
}

func (m *settlementMsg) Data() []byte    { return m.data }
func (m *settlementMsg) Subject() string { return m.subject }
func (m *settlementMsg) Metadata() (*jetstream.MsgMetadata, error) {
	if m.metaErr != nil {
		return nil, m.metaErr
	}
	return &jetstream.MsgMetadata{NumDelivered: m.delivered}, nil
}
func (*settlementMsg) Headers() nats.Header            { return nil }
func (*settlementMsg) Reply() string                   { return "" }
func (*settlementMsg) DoubleAck(context.Context) error { return nil }
func (m *settlementMsg) Ack() error                    { m.acks.Add(1); return nil }
func (m *settlementMsg) Nak() error                    { m.naks.Add(1); return nil }
func (m *settlementMsg) NakWithDelay(delay time.Duration) error {
	m.naks.Add(1)
	m.nakDelays = append(m.nakDelays, delay)
	return nil
}
func (m *settlementMsg) InProgress() error           { m.heartbeat.Add(1); return nil }
func (m *settlementMsg) Term() error                 { m.terms.Add(1); return nil }
func (m *settlementMsg) TermWithReason(string) error { return m.Term() }

func (m *settlementMsg) settlements() int32 {
	return m.acks.Load() + m.naks.Load() + m.terms.Load()
}

// --- fakes ------------------------------------------------------------------

// stubRunReader answers Manager.Get from a script. It exists so a resolution
// condition the real Manager only reaches against live NATS (a run that is not
// lifecycle-managed yet) can be presented to the classifier.
type stubRunReader struct {
	participant lifecycle.Participant
	err         error
}

func (r *stubRunReader) Get(context.Context, string, string) (lifecycle.Participant, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.participant, nil
}

// foreignParticipant is a lifecycle.Participant that is not an *AgentRun — the
// registration defect the resolution_type row terminates on.
type foreignParticipant struct{}

func (foreignParticipant) EntityID() string       { return "acme.ops.chain.agent.execution.foreign" }
func (foreignParticipant) Workflow() string       { return WorkflowName }
func (foreignParticipant) Phase() string          { return "executing" }
func (foreignParticipant) IsTerminal() bool       { return false }
func (foreignParticipant) ParentEntityID() string { return "" }

// stubTripleReader has no triples, so ResolveRun always takes the ancestry walk
// and ends at the loop itself.
type stubTripleReader struct{ err error }

func (r stubTripleReader) GetLoopRunID(context.Context, string) (string, bool, error) {
	return "", false, r.err
}

func (r stubTripleReader) GetLoopParentEntityID(context.Context, string) (string, bool, error) {
	return "", false, r.err
}

// handlerFunc adapts a function to MilestoneHandler.
type handlerFunc func(context.Context, LoopTerminalEvent, *AgentRun) error

func (f handlerFunc) OnLoopTerminal(ctx context.Context, ev LoopTerminalEvent, run *AgentRun) error {
	return f(ctx, ev, run)
}

// terminalBytes is one production wire envelope for a completed loop.
func terminalBytes(t *testing.T, loopID, runEntityID string) []byte {
	t.Helper()
	completed := &agentic.LoopCompletedEvent{
		LoopID:      loopID,
		TaskID:      "task-" + loopID,
		Outcome:     agentic.OutcomeSuccess,
		Role:        "researcher",
		CompletedAt: time.Now().UTC(),
		RunEntityID: runEntityID,
	}
	data, err := json.Marshal(message.NewBaseMessage(completed.Schema(), completed, "agentic-loop"))
	require.NoError(t, err)
	return data
}

// quietSubscriber builds a subscriber whose logs go nowhere, for the tests that
// assert a decision rather than an emission.
func quietSubscriber(runs RunStateReader, reader LoopTripleReader, org string) *MilestoneSubscriber {
	logger := slog.New(slog.NewTextHandler(discard{}, nil))
	return NewMilestoneSubscriberWithRunStateReader(runs, reader, org, "ops", logger)
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// reasonOf runs one attempt through the decision path and returns what it
// decided and why. The settlement method alone does not identify the row: four
// different rows terminate and three retry, so a test that only counts Terms
// cannot tell the row it means from a decode failure.
func reasonOf(t *testing.T, s *MilestoneSubscriber, data []byte) (natsclient.DeliveryDecision, string) {
	t.Helper()
	decided := s.decide(t.Context(), data)
	return decided.decision, decided.reason
}

// --- 3.5: the fanout settles as one unit -----------------------------------

// TestMilestoneFanoutAcksOnlyWhenEveryHandlerReturnsNil is invariant I1. The
// second handler is the one that decides, and it is the SAME delivery both
// times, so an implementation that settles per handler or on the first one
// cannot pass both halves.
func TestMilestoneFanoutAcksOnlyWhenEveryHandlerReturnsNil(t *testing.T) {
	t.Parallel()
	runEntityID := agentic.ChainExecutionEntityID("acme", "ops", "unit-run")
	run := &AgentRun{EntityIDField: runEntityID, PhaseField: "executing"}

	var second error
	var calls atomic.Int32
	sub := quietSubscriber(&stubRunReader{participant: run}, stubTripleReader{}, "acme")
	sub.AddHandler(handlerFunc(func(context.Context, LoopTerminalEvent, *AgentRun) error {
		calls.Add(1)
		return nil
	}))
	sub.AddHandler(handlerFunc(func(context.Context, LoopTerminalEvent, *AgentRun) error {
		calls.Add(1)
		return second
	}))
	lane := newMilestoneLaneFixture(t, sub, milestoneLaneComplete)
	data := terminalBytes(t, "unit-loop", runEntityID)

	second = semerrs.WrapTransient(errors.New("store not ready"), "product", "OnLoopTerminal", "commit")
	blocked := lane.deliver(t, data)
	assert.Zero(t, blocked.acks.Load(), "one handler short of done must not acknowledge")
	assert.Equal(t, int32(1), blocked.naks.Load(), "a transient outcome redelivers")
	assert.Equal(t, []time.Duration{milestoneRetryDelay}, blocked.nakDelays,
		"the Nak carries the lane's retry delay, not line-rate redelivery")
	assert.Equal(t, int32(2), calls.Load(), "every handler runs on every attempt")

	second = nil
	acked := lane.deliver(t, data)
	assert.Equal(t, int32(1), acked.acks.Load(), "an attempt where every handler returned nil acknowledges")
	assert.Equal(t, int32(1), acked.settlements(), "exactly one terminal method per delivery")
	assert.Equal(t, int32(4), calls.Load(), "the replay re-ran BOTH handlers, including the one already done")
}

// TestMilestoneFanoutRetriesOnTransientHandlerError pins the transient row: a
// handler that is not ready yet gets the delivery back after the lane's delay.
func TestMilestoneFanoutRetriesOnTransientHandlerError(t *testing.T) {
	t.Parallel()
	sub := quietSubscriber(&stubRunReader{err: lifecycle.ErrEntityNotFound}, stubTripleReader{}, "acme")
	sub.AddHandler(handlerFunc(func(context.Context, LoopTerminalEvent, *AgentRun) error {
		return semerrs.WrapTransient(errors.New("downstream busy"), "product", "OnLoopTerminal", "commit")
	}))
	lane := newMilestoneLaneFixture(t, sub, milestoneLaneComplete)

	data := terminalBytes(t, "transient-loop", "")
	msg := lane.deliver(t, data)
	assert.Equal(t, int32(1), msg.naks.Load(), "a transient handler outcome redelivers")
	assert.Zero(t, msg.acks.Load()+msg.terms.Load())
	assert.NoError(t, sub.DeliveryFatal(), "a retry never latches the lane")
	decision, reason := reasonOf(t, sub, data)
	assert.Equal(t, natsclient.DeliveryDecisionRetry, decision)
	assert.Equal(t, reasonHandlerTransient, reason)
}

// TestMilestoneFanoutTerminatesOnAllInvalid is owner ruling O1: invalid is a
// per-message judgment, so an attempt where every handler rejected the payload
// terminates THAT delivery instead of latching the lane for one bad message.
func TestMilestoneFanoutTerminatesOnAllInvalid(t *testing.T) {
	t.Parallel()
	sub := quietSubscriber(&stubRunReader{err: lifecycle.ErrEntityNotFound}, stubTripleReader{}, "acme")
	for i := 0; i < 2; i++ {
		sub.AddHandler(handlerFunc(func(context.Context, LoopTerminalEvent, *AgentRun) error {
			return semerrs.WrapInvalid(errors.New("payload rejected"), "product", "OnLoopTerminal", "validate")
		}))
	}
	lane := newMilestoneLaneFixture(t, sub, milestoneLaneComplete)

	data := terminalBytes(t, "invalid-loop", "")
	msg := lane.deliver(t, data)
	assert.Equal(t, int32(1), msg.terms.Load(), "an all-invalid attempt terminates the delivery")
	assert.Zero(t, msg.acks.Load()+msg.naks.Load())
	assert.NoError(t, sub.DeliveryFatal(), "one bad payload must not latch the lane")
	decision, reason := reasonOf(t, sub, data)
	assert.Equal(t, natsclient.DeliveryDecisionTerminate, decision)
	assert.Equal(t, reasonHandlerInvalid, reason, "the counter must say a handler rejected it, not that it failed to decode")
}

// TestMilestoneFanoutQuarantinesOnHandlerPanic is invariant I3: a quarantined
// delivery is never settled by this owner, the lane stops admitting work, and
// the cause reaches DeliveryFatal.
func TestMilestoneFanoutQuarantinesOnHandlerPanic(t *testing.T) {
	t.Parallel()
	var secondRan atomic.Bool
	sub := quietSubscriber(&stubRunReader{err: lifecycle.ErrEntityNotFound}, stubTripleReader{}, "acme")
	sub.AddHandler(handlerFunc(func(context.Context, LoopTerminalEvent, *AgentRun) error {
		panic("product handler exploded")
	}))
	sub.AddHandler(handlerFunc(func(context.Context, LoopTerminalEvent, *AgentRun) error {
		secondRan.Store(true)
		return nil
	}))
	lane := newMilestoneLaneFixture(t, sub, milestoneLaneComplete)

	msg := lane.deliver(t, terminalBytes(t, "panic-loop", ""))
	assert.Zero(t, msg.settlements(), "a quarantined delivery is left to JetStream: no Ack, Nak or Term")
	assert.True(t, secondRan.Load(), "the panic guard keeps the remaining handlers running")
	require.Error(t, sub.DeliveryFatal(), "the fatal must reach the owner's health record")
	assert.False(t, lane.admission.Admit(), "the lane admits no further local work")
}

// --- 3.6: resolution ---------------------------------------------------------

// TestMilestoneNotManagedEntityRetriesThenAcksAfterCreate is R2 / invariant I7:
// the ADR-049 forward reference is a documented-transient condition, so it
// retries until a later Create writes the phase triple, and then acknowledges.
func TestMilestoneNotManagedEntityRetriesThenAcksAfterCreate(t *testing.T) {
	t.Parallel()
	runEntityID := agentic.ChainExecutionEntityID("acme", "ops", "late-run")
	// The wrap is Manager.getWithRevision's: the sentinel arrives inside a
	// formatted chain, never bare, so the classifier has to match through it.
	reader := &stubRunReader{err: fmt.Errorf("%w: workflow=%q entity_id=%q (no agent.run.phase triple)",
		lifecycle.ErrEntityNotLifecycleManaged, WorkflowName, runEntityID)}
	var handled atomic.Int32
	sub := quietSubscriber(reader, stubTripleReader{}, "acme")
	sub.AddHandler(handlerFunc(func(context.Context, LoopTerminalEvent, *AgentRun) error {
		handled.Add(1)
		return nil
	}))
	lane := newMilestoneLaneFixture(t, sub, milestoneLaneComplete)
	data := terminalBytes(t, "late-loop", runEntityID)

	decision, reason := reasonOf(t, sub, data)
	assert.Equal(t, natsclient.DeliveryDecisionRetry, decision)
	assert.Equal(t, reasonNotManaged, reason)

	early := lane.deliver(t, data)
	assert.Equal(t, int32(1), early.naks.Load(), "a not-yet-managed entity retries")
	assert.Zero(t, early.acks.Load()+early.terms.Load(), "it is neither dropped nor terminated")
	assert.Zero(t, handled.Load(), "no handler sees a run that is not lifecycle-managed")

	// A later Manager.Create writes the phase triple.
	reader.err = nil
	reader.participant = &AgentRun{EntityIDField: runEntityID, PhaseField: "executing"}

	late := lane.deliver(t, data)
	assert.Equal(t, int32(1), late.acks.Load(), "the replay resolves and acknowledges")
	assert.Equal(t, int32(1), handled.Load(), "the handler runs once the run is readable")
}

// TestMilestoneUnregisteredWorkflowQuarantinesAndLatches is R1, driven through
// the REAL lifecycle.Manager: a Manager that never registered the agent-run
// workflow answers with the real ErrWorkflowNotRegistered. The condition is
// process-wide — WorkflowName is a package constant — so terminating would
// silently drop every milestone this process ever sees.
func TestMilestoneUnregisteredWorkflowQuarantinesAndLatches(t *testing.T) {
	t.Parallel()
	unregistered := lifecycle.NewManager(nil, slog.New(slog.NewTextHandler(discard{}, nil)))
	sub := quietSubscriber(unregistered, stubTripleReader{}, "acme")
	lane := newMilestoneLaneFixture(t, sub, milestoneLaneComplete)

	data := terminalBytes(t, "orphan-loop", agentic.ChainExecutionEntityID("acme", "ops", "orphan-run"))
	decision, reason := reasonOf(t, sub, data)
	assert.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
	assert.Equal(t, reasonComposition, reason)

	msg := lane.deliver(t, data)
	assert.Zero(t, msg.settlements(), "every milestone stays unacked and redeliverable")
	require.Error(t, sub.DeliveryFatal(), "a composition defect must reach health")
	assert.ErrorIs(t, sub.DeliveryFatal(), lifecycle.ErrWorkflowNotRegistered,
		"the latched cause names the composition defect")
	assert.False(t, lane.admission.Admit())
}

// TestMilestoneResolutionInvalidTerminates covers the deterministic AND
// matchable resolution failures: entity-ID grammar, a non-string predicate
// value, and a participant that is not an *AgentRun. Each is poison for this
// delivery no matter how often it is replayed.
func TestMilestoneResolutionInvalidTerminates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		runs       RunStateReader
		reader     LoopTripleReader
		org        string
		loopID     string
		wantReason string
	}{
		{
			name:       "entity-ID grammar",
			wantReason: reasonResolutionInvalid,
			runs:       &stubRunReader{err: lifecycle.ErrEntityNotFound},
			reader:     stubTripleReader{},
			org:        "acme",
			// A dotted loop id cannot become a 6-part entity ID.
			loopID: "loop.with.dots",
		},
		{
			name:       "non-string predicate value",
			wantReason: reasonResolutionInvalid,
			runs:       &stubRunReader{err: lifecycle.ErrEntityNotFound},
			// The class NATSLoopTripleReader assigns a non-string triple value.
			reader: stubTripleReader{err: semerrs.WrapInvalid(
				errors.New(`predicate "agent.loop.run" has non-string value int`),
				"agentrun", "NATSLoopTripleReader", "read string triple")},
			org:    "acme",
			loopID: "typed-loop",
		},
		{
			name:       "participant is not an AgentRun",
			wantReason: reasonResolutionType,
			runs:       &stubRunReader{participant: foreignParticipant{}},
			reader:     stubTripleReader{},
			org:        "acme",
			loopID:     "foreign-loop",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sub := quietSubscriber(tc.runs, tc.reader, tc.org)
			lane := newMilestoneLaneFixture(t, sub, milestoneLaneComplete)
			data := terminalBytes(t, tc.loopID, "")
			decision, reason := reasonOf(t, sub, data)
			assert.Equal(t, natsclient.DeliveryDecisionTerminate, decision)
			assert.Equal(t, tc.wantReason, reason,
				"the row that terminated must be the resolution row, not a decode failure")
			msg := lane.deliver(t, data)
			assert.Equal(t, int32(1), msg.terms.Load(), "a deterministic resolution defect terminates")
			assert.Zero(t, msg.acks.Load()+msg.naks.Load())
			assert.NoError(t, sub.DeliveryFatal(), "poison for one delivery must not latch the lane")
		})
	}
}

// TestMilestoneNilReaderAndProjectionFailuresRetry pins the unmatchable half of
// the matrix: a resolution failure the errs classes cannot place retries under
// the lane's finite MaxDeliver rather than being dropped.
func TestMilestoneNilReaderAndProjectionFailuresRetry(t *testing.T) {
	t.Parallel()
	// The nil exact reader is produced by the REAL Manager: a test-mode Manager
	// built without a NATS client has no exact reader, and the error it returns
	// carries no sentinel and no errs class at all.
	nilReader := lifecycle.NewManager(nil, slog.New(slog.NewTextHandler(discard{}, nil)))
	require.NoError(t, Register(nilReader))

	// The projection failure's SHAPE, as pkg/lifecycle emits it from
	// getWithRevision when projectTriples cannot fill the registered schema
	// type. Like the nil reader it carries no sentinel; unlike a decode failure
	// it is not proven to be poison.
	projectionFailure := &stubRunReader{err: fmt.Errorf(
		"lifecycle: project entity %q (workflow %q): %w",
		agentic.ChainExecutionEntityID("acme", "ops", "bent-run"), WorkflowName,
		errors.New("field PhaseField: cannot assign 7 to string"))}

	cases := []struct {
		name string
		runs RunStateReader
	}{
		{name: "exact reader unavailable", runs: nilReader},
		{name: "projection into the schema type", runs: projectionFailure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sub := quietSubscriber(tc.runs, stubTripleReader{}, "acme")
			lane := newMilestoneLaneFixture(t, sub, milestoneLaneComplete)
			data := terminalBytes(t, "unreadable-loop",
				agentic.ChainExecutionEntityID("acme", "ops", "unreadable-run"))
			decision, reason := reasonOf(t, sub, data)
			assert.Equal(t, natsclient.DeliveryDecisionRetry, decision)
			assert.Equal(t, reasonResolutionTransient, reason,
				"an unmatchable failure must not borrow the not_managed reason")
			msg := lane.deliver(t, data)
			assert.Equal(t, int32(1), msg.naks.Load(), "an unclassifiable resolution failure retries")
			assert.Zero(t, msg.acks.Load()+msg.terms.Load(), "it is neither acknowledged nor dropped")
			assert.NoError(t, sub.DeliveryFatal(), "a bounded retry never latches the lane")
		})
	}
}

// --- 3.7: observation --------------------------------------------------------

// TestMilestoneNonAckDecisionsLogOnceAndCountOnce is invariant I5. Both halves
// matter: the log line has to carry enough identity to find the delivery, and
// the counter has to move exactly once so a rate is a rate.
func TestMilestoneNonAckDecisionsLogOnceAndCountOnce(t *testing.T) {
	t.Parallel()
	var logs strings.Builder
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	runEntityID := agentic.ChainExecutionEntityID("acme", "ops", "observed-run")
	sub := NewMilestoneSubscriberWithRunStateReader(
		&stubRunReader{participant: &AgentRun{EntityIDField: runEntityID, PhaseField: "executing"}},
		stubTripleReader{}, "acme", "ops", logger,
	)
	sub.AddHandler(handlerFunc(func(context.Context, LoopTerminalEvent, *AgentRun) error {
		return semerrs.WrapTransient(errors.New("not ready"), "product", "OnLoopTerminal", "commit")
	}))
	lane := newMilestoneLaneFixture(t, sub, milestoneLaneFailed)

	data := terminalBytes(t, "observed-loop", runEntityID)
	lane.deliver(t, data)

	decisionLines := decisionLogLines(t, logs.String())
	require.Len(t, decisionLines, 1, "one non-Ack decision emits exactly one decision line")
	line := decisionLines[0]
	assert.NotEmpty(t, line["source_message_id"], "the line must name the delivery identity")
	assert.Equal(t, "observed-loop", line["loop_id"])
	assert.Equal(t, agentic.CategoryLoopCompleted, line["category"])
	assert.Equal(t, milestoneLaneFailed, line["lane"])
	assert.Equal(t, reasonHandlerTransient, line["reason"])

	counter := sub.decisions.WithLabelValues(milestoneLaneFailed, "retry", reasonHandlerTransient)
	assert.InDelta(t, 1.0, testutil.ToFloat64(counter), 0.0,
		"one non-Ack decision is one increment")

	// An acknowledged delivery says nothing and counts nothing.
	logs.Reset()
	sub.handlers = nil
	sub.AddHandler(handlerFunc(func(context.Context, LoopTerminalEvent, *AgentRun) error { return nil }))
	lane.deliver(t, data)
	assert.Empty(t, decisionLogLines(t, logs.String()), "an Ack emits no decision line")
	assert.InDelta(t, 1.0, testutil.ToFloat64(counter), 0.0, "an Ack does not move the counter")
}

// TestMilestoneRefusedDeliveryIsNotLoggedAsASettlementFailure pins the guard
// reconciliation B1 names. deliverylane.Consume answers a refused delivery with
// the ZERO DeliveryResult, and a zero result's Err() is non-nil by
// construction, so an unguarded `result.Err() != nil` branch reports a delivery
// that ran no work and attempted no terminal method as a settlement failure —
// exactly the class of noise an operator uses to decide a lane is broken.
//
// It drives consumeLane, the closure the NATS callback actually is, not the
// DeliveryWork underneath it.
func TestMilestoneRefusedDeliveryIsNotLoggedAsASettlementFailure(t *testing.T) {
	t.Parallel()
	var logs strings.Builder
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	var handlerCalls atomic.Int32
	sub := NewMilestoneSubscriberWithRunStateReader(
		&stubRunReader{err: lifecycle.ErrEntityNotFound}, stubTripleReader{}, "acme", "ops", logger)
	sub.AddHandler(handlerFunc(func(context.Context, LoopTerminalEvent, *AgentRun) error {
		handlerCalls.Add(1)
		panic("product handler exploded")
	}))
	admission := sub.newLaneAdmission()
	callback := sub.consumeLane(milestoneLaneComplete, milestonePolicyFor(t, sub, milestoneLaneComplete), admission)

	// First delivery latches the lane.
	first := &settlementMsg{data: terminalBytes(t, "latching-loop", ""), subject: "agent.complete.x", delivered: 1}
	callback(t.Context(), first)
	require.False(t, admission.Admit(), "the panic must close the lane")
	require.Equal(t, int32(1), handlerCalls.Load())

	// Second delivery arrives at a closed lane: buffered deliveries still reach
	// the callback after the latch, before the drain has flushed them.
	logs.Reset()
	second := &settlementMsg{data: terminalBytes(t, "refused-loop", ""), subject: "agent.complete.x", delivered: 1}
	callback(t.Context(), second)

	assert.Equal(t, int32(1), handlerCalls.Load(), "a refused delivery runs no work")
	assert.Zero(t, second.settlements(), "a refused delivery attempts no terminal method")
	assert.Zero(t, second.heartbeat.Load(), "a refused delivery is not heartbeaten")
	assert.Empty(t, logLinesWithMessage(t, logs.String(), "agentrun: milestone delivery did not settle cleanly"),
		"a refusal is not a settlement failure")
	assert.Empty(t, decisionLogLines(t, logs.String()), "a refusal reached no decision to report")
}

// logLinesWithMessage returns the records whose slog message is exactly msg.
func logLinesWithMessage(t *testing.T, raw, msg string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, entry := range strings.Split(strings.TrimSpace(raw), "\n") {
		if entry == "" {
			continue
		}
		record := map[string]any{}
		require.NoError(t, json.Unmarshal([]byte(entry), &record), "log entry: %s", entry)
		if record["msg"] == msg {
			lines = append(lines, record)
		}
	}
	return lines
}

// decisionLogLines returns the records carrying a decision reason, which is the
// field only observeDecision writes.
func decisionLogLines(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, entry := range strings.Split(strings.TrimSpace(raw), "\n") {
		if entry == "" {
			continue
		}
		record := map[string]any{}
		require.NoError(t, json.Unmarshal([]byte(entry), &record), "log entry: %s", entry)
		if _, ok := record["reason"]; ok {
			lines = append(lines, record)
		}
	}
	return lines
}
