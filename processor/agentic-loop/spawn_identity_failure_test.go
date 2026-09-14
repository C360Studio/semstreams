package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/message"
)

type spawnBirthProbeBucket struct {
	*terminalSelectionBucket
	operations []string
}

func (b *spawnBirthProbeBucket) Create(ctx context.Context, key string, data []byte, opts ...jetstream.KVCreateOpt) (uint64, error) {
	revision, err := b.terminalSelectionBucket.Create(ctx, key, data, opts...)
	if err == nil {
		operation := "create:" + key
		if !strings.HasPrefix(key, "COMPLETE_") {
			var entity agentic.LoopEntity
			if decodeErr := json.Unmarshal(data, &entity); decodeErr != nil {
				return 0, decodeErr
			}
			operation += ":" + string(entity.State)
		}
		b.operations = append(b.operations, operation)
	}
	return revision, err
}

func (b *spawnBirthProbeBucket) Update(ctx context.Context, key string, data []byte, revision uint64) (uint64, error) {
	updated, err := b.terminalSelectionBucket.Update(ctx, key, data, revision)
	if err == nil {
		b.operations = append(b.operations, "update:"+key)
	}
	return updated, err
}

type inputAckMsg struct {
	data       []byte
	acked      atomic.Bool
	naked      atomic.Bool
	nakCount   atomic.Int32
	nakDelay   atomic.Int64
	inProgress atomic.Int32
	terminated atomic.Bool
	progress   chan struct{}
}

func (m *inputAckMsg) Data() []byte         { return m.data }
func (m *inputAckMsg) Subject() string      { return "agent.task.test" }
func (m *inputAckMsg) Reply() string        { return "" }
func (m *inputAckMsg) Headers() nats.Header { return nil }
func (m *inputAckMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return &jetstream.MsgMetadata{NumDelivered: 1}, nil
}
func (m *inputAckMsg) Ack() error {
	m.acked.Store(true)
	return nil
}
func (m *inputAckMsg) DoubleAck(context.Context) error { return nil }
func (m *inputAckMsg) Nak() error {
	m.naked.Store(true)
	m.nakCount.Add(1)
	return nil
}
func (m *inputAckMsg) NakWithDelay(delay time.Duration) error {
	m.naked.Store(true)
	m.nakCount.Add(1)
	m.nakDelay.Store(int64(delay))
	return nil
}
func (m *inputAckMsg) InProgress() error {
	m.inProgress.Add(1)
	if m.progress != nil {
		m.progress <- struct{}{}
	}
	return nil
}
func (m *inputAckMsg) Term() error {
	m.terminated.Store(true)
	return nil
}
func (m *inputAckMsg) TermWithReason(string) error {
	m.terminated.Store(true)
	return nil
}

// Per-entity poison semantics (poison-response-scoping D9): the typed
// graph_state_reset_required classification means THIS loop's entity is
// poisoned. The loop fails through the normal terminal business-failure
// path with the typed error preserved; no component-wide state changes.
func TestHandleSpawnIdentityFailure_GraphStatePoisonFailsLoopPerEntity(t *testing.T) {
	t.Parallel()

	handler := NewMessageHandler(DefaultConfig())
	// A loop instance token is framework-minted (ADR-105, #1192), so the fixture
	// asks the loop manager for one rather than authoring a readable name.
	loopID := handler.loopManager.GenerateLoopID()
	if _, err := handler.loopManager.CreateLoopWithID(loopID, "task-poison", "researcher", "model"); err != nil {
		t.Fatalf("CreateLoopWithID() error = %v", err)
	}
	before, err := handler.loopManager.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop() before error = %v", err)
	}
	if _, err := handler.trajectoryManager.startTrajectory(loopID); err != nil {
		t.Fatalf("StartTrajectory() error = %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler.logger = logger
	c := &Component{
		handler:   handler,
		config:    DefaultConfig(),
		logger:    logger,
		started:   true,
		startTime: time.Now(),
	}
	// The loop's terminal state is read at the terminal OBSERVATION, not from
	// the loop manager afterwards: a settled loop's per-loop state is released
	// at terminal (#1233), so the map is empty by the time this returns. The
	// probe snapshots the entity as the terminal fact is written, which is
	// inside the window every terminal reader shares.
	probe := newTerminalReaderProbe(c, loopID)
	poison := errs.ClassifiedCode(errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired,
		&graph.StateContractError{Reason: graph.GraphStateReasonNoncanonicalEntityID})

	if _, gotErr := c.handleSpawnIdentityFailure(context.Background(), loopID, before, poison); gotErr != nil {
		t.Fatalf("handleSpawnIdentityFailure() error = %v, want nil (per-loop failure is fully handled)", gotErr)
	}

	after := probe.terminalLoop(t)
	if after.State != agentic.LoopStateFailed || after.Outcome != agentic.OutcomeFailed {
		t.Fatalf("poisoned loop state=%q outcome=%q, want terminal business failure", after.State, after.Outcome)
	}
	if !strings.Contains(after.Error, graph.ErrorCodeGraphStateResetRequired) {
		t.Fatalf("failed loop Error = %q, want the typed %q code preserved", after.Error, graph.ErrorCodeGraphStateResetRequired)
	}
	if _, err := handler.loopManager.GetLoop(loopID); err == nil {
		t.Fatal("settled loop still held in process memory after the terminal path released it")
	}

	// One poisoned entity must not degrade the component: Health stays
	// healthy and no component-wide latch blocks subsequent task intake.
	//
	// The probe recorder is dropped first. It is a test seam with no store
	// registry, and Health reports an unavailable evidence provider for exactly
	// that — a fact about the fixture, not about the poison this asserts on.
	c.trajectoryRecorder = nil
	if health := c.Health(); !health.Healthy || health.Status != "running" {
		t.Fatalf("Health() = %#v, want healthy running (per-entity poison must not degrade the component)", health)
	}
}

func TestGraphStatePoisonRouting_DistinguishesOperationalErrors(t *testing.T) {
	t.Parallel()

	poison := &graph.StateContractError{Reason: graph.GraphStateReasonUnreadableEntity}
	if !graph.IsStateContractError(poison) {
		t.Fatal("StateContractError must route to the per-loop poison failure path")
	}
	remotePoison := errs.ClassifiedCode(errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired,
		errors.New("remote entity poison classification"))
	if !graph.IsStateContractError(remotePoison) {
		t.Fatal("wire-reconstructed graph_state_reset_required code must route to the per-loop poison failure path")
	}

	operational := errors.New("request timeout")
	if graph.IsStateContractError(operational) {
		t.Fatal("ordinary operational errors must keep the spawn_identity_birth_failed reason")
	}
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestHandleSpawnIdentityFailure_InvalidSerializationTerminatesAndDiscardsSpeculativeState(t *testing.T) {
	t.Parallel()

	loopManager := NewLoopManager()
	loopID := loopManager.GenerateLoopID()
	if _, err := loopManager.CreateLoopWithID(loopID, "task-operational", "researcher", "model"); err != nil {
		t.Fatalf("CreateLoopWithID() error = %v", err)
	}
	entity, err := loopManager.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop() before error = %v", err)
	}
	// Unserializable initial state refuses before durable birth, so no terminal
	// observation or selected failure may advertise a loop that was never persisted.
	entity.Metadata = map[string]any{"unserializable": func() {}}
	if err := loopManager.UpdateLoop(entity); err != nil {
		t.Fatalf("UpdateLoop() error = %v", err)
	}

	bucket := &terminalSelectionBucket{&approvalRevisionBucket{
		settlementBucket: &settlementBucket{values: make(map[string][]byte)},
		revisions:        make(map[string]uint64),
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := &Component{
		handler: &MessageHandler{
			loopManager:       loopManager,
			trajectoryManager: newTrajectoryManager(),
			logger:            logger,
		},
		logger:      logger,
		config:      DefaultConfig(),
		loopsBucket: bucket,
	}
	decision, err := c.handleSpawnIdentityFailure(context.Background(), loopID, entity, errors.New("temporary graph request failure"))
	if err == nil || decision != natsclient.DeliveryDecisionTerminate {
		t.Fatalf("serialization failure settlement = (%v, %v), want terminate with error", decision, err)
	}

	require.ErrorContains(t, err, "marshal initial loop", "must refuse at the pre-birth serialization boundary")
	require.NotContains(t, bucket.values, loopID)
	require.NotContains(t, bucket.values, "COMPLETE_"+loopID)
	require.Empty(t, bucket.values, "invalid birth must not author any durable evidence")
	if _, err := loopManager.GetLoop(loopID); err == nil {
		t.Fatal("failed failure serialization retained speculative process state before durable settlement")
	}
}

// TestGraphStatePoisonFailsLoopWhileIntakeContinues drives the production
// task-intake path end to end (envelope decode → HandleTask → graph birth →
// lineage write) for two tasks. The first task's graph write returns the
// typed poison classification: that loop fails terminally with the typed
// error and its delivery is ACKed. The second task — a different loop /
// different entity — must process normally, proving no component-wide latch
// wedges task intake and Health stays healthy. This deliberately inverts the
// retired hold-until-restart behavior (poison-response-scoping D9).
func TestGraphStatePoisonFailsLoopWhileIntakeContinues(t *testing.T) {
	configJSON, err := json.Marshal(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	discoverable, err := NewComponent(configJSON, component.Dependencies{
		Platform:        component.PlatformMeta{Org: "acme", Platform: "ops"},
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	c := discoverable.(*Component)
	// NATS-less Start marks the component running so Health reflects the
	// steady state the assertions below depend on.
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.Stop(context.Background())

	// Both loops carry framework-minted tokens (ADR-105, #1192): these tasks go
	// out through the production BaseMessage envelope, which validates them.
	poisonedLoopID := c.handler.loopManager.GenerateLoopID()
	healthyLoopID := c.handler.loopManager.GenerateLoopID()

	bucket := &spawnBirthProbeBucket{terminalSelectionBucket: &terminalSelectionBucket{&approvalRevisionBucket{
		settlementBucket: &settlementBucket{values: make(map[string][]byte)},
		revisions:        make(map[string]uint64),
	}}}
	c.loopsBucket = bucket
	c.settlementEvidence = &settlementEvidence{}

	poison := errs.ClassifiedCode(errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired,
		&graph.StateContractError{Reason: graph.GraphStateReasonNoncanonicalEntityID})
	var lineageWrites atomic.Int32
	c.testLineageWriteHook = func(_ context.Context, loopID string, _ map[string]any) error {
		lineageWrites.Add(1)
		if loopID == poisonedLoopID {
			require.NotContains(t, bucket.values, loopID, "fixture must reach the actual pre-birth failure")
			require.NotContains(t, bucket.values, "COMPLETE_"+loopID)
			return poison
		}
		return nil
	}

	consumeTask := func(loopID, taskID string) *inputAckMsg {
		t.Helper()
		task := validLineageTask(taskID)
		task.LoopID = loopID
		task.Metadata = map[string]any{
			agentic.MetadataKeyRelatedLoops: map[string]any{"researcher": "upstream-loop"},
		}
		envelope := message.NewBaseMessage(task.Schema(), &task, "test")
		data, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		msg := &inputAckMsg{data: data}
		if err := consumeTypedLongRunningInput(context.Background(), msg, time.Hour,
			c.taskInputHandler(time.Minute)); err != nil {
			t.Fatalf("consume(%s) error = %v, want nil", taskID, err)
		}
		return msg
	}

	// Task 1: touches the poisoned entity. The loop fails terminally with
	// the typed error; the delivery is ACKed per the loop-failure
	// convention (never held in flight, never Term'd as producer fault).
	//
	// The terminal outcome is read at the terminal observation: a settled
	// loop's per-loop state is released (#1233), so the loop manager no longer
	// holds it once intake returns.
	probe := newTerminalReaderProbe(c, poisonedLoopID)
	first := consumeTask(poisonedLoopID, "task-poisoned-entity")
	if !first.acked.Load() || first.naked.Load() || first.terminated.Load() {
		t.Fatalf("poisoned-loop delivery ack state: ack=%v nak=%v term=%v, want ACK only",
			first.acked.Load(), first.naked.Load(), first.terminated.Load())
	}
	failed := probe.terminalLoop(t)
	require.Equal(t, []string{"create:" + poisonedLoopID + ":running", "create:COMPLETE_" + poisonedLoopID, "update:" + poisonedLoopID}, bucket.operations,
		"real running birth, selected failure, then final marker must be the exact write sequence")
	var durable agentic.LoopEntity
	require.NoError(t, json.Unmarshal(bucket.values[poisonedLoopID], &durable))
	require.NoError(t, durable.Validate())
	require.Equal(t, agentic.LoopStateFailed, durable.State)
	require.Equal(t, "task-poisoned-entity", durable.TaskID)
	var selected agentic.LoopFailedEvent
	require.NoError(t, json.Unmarshal(bucket.values["COMPLETE_"+poisonedLoopID], &selected))
	require.NoError(t, selected.Validate())
	require.Equal(t, agentic.OutcomeFailed, selected.Outcome)
	require.Equal(t, poisonedLoopID, selected.LoopID)
	require.Equal(t, "task-poisoned-entity", selected.TaskID)
	require.Contains(t, selected.Error, graph.ErrorCodeGraphStateResetRequired)
	if failed.State != agentic.LoopStateFailed || failed.Outcome != agentic.OutcomeFailed {
		t.Fatalf("poisoned loop state=%q outcome=%q, want terminal failure", failed.State, failed.Outcome)
	}
	if !strings.Contains(failed.Error, graph.ErrorCodeGraphStateResetRequired) {
		t.Fatalf("poisoned loop Error = %q, want the typed %q code preserved", failed.Error, graph.ErrorCodeGraphStateResetRequired)
	}
	if _, err := c.handler.GetLoop(poisonedLoopID); err == nil {
		t.Fatal("the settled poisoned loop is still held in process memory; terminal release did not run")
	}

	// Task 2: a different loop over a different entity processes normally —
	// task intake was never wedged by the first loop's poison.
	second := consumeTask(healthyLoopID, "task-healthy-entity")
	if !second.acked.Load() || second.naked.Load() || second.terminated.Load() {
		t.Fatalf("healthy-loop delivery ack state: ack=%v nak=%v term=%v, want ACK only",
			second.acked.Load(), second.naked.Load(), second.terminated.Load())
	}
	healthy, err := c.handler.GetLoop(healthyLoopID)
	if err != nil {
		t.Fatalf("GetLoop(%s) error = %v", healthyLoopID, err)
	}
	if healthy.State == agentic.LoopStateFailed {
		t.Fatalf("healthy loop state = %q, want non-failed active state", healthy.State)
	}
	if got := lineageWrites.Load(); got != 2 {
		t.Fatalf("lineage writes = %d, want 2 (second task reached its graph write)", got)
	}

	// Health never degrades for this class: per-entity poison is a loop
	// outcome, not a component condition. The probe recorder is dropped first —
	// it has no store registry, and Health would report that fixture fact
	// rather than anything about the poison.
	c.trajectoryRecorder = nil
	if health := c.Health(); !health.Healthy || health.Status != "running" {
		t.Fatalf("Health() = %#v, want healthy running after a poisoned loop", health)
	}
}

func TestCleanupAfterStartFailureResetsState(t *testing.T) {
	t.Parallel()

	c := &Component{
		natsClient:    &natsclient.Client{},
		consumerInfos: []consumerInfo{{streamName: "AGENT", consumerName: "partial-start"}},
	}

	if err := c.cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup consumers: %v", err)
	}
	c.clearLifecycleHandles()

	if c.consumerInfos != nil {
		t.Fatalf("partial-start consumer state not reset: infos=%v", c.consumerInfos)
	}
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestSpawnBirthCreateFailureCannotDeduplicateRedelivery(t *testing.T) {
	discoverable, err := NewComponent([]byte("{}"), component.Dependencies{
		Platform:        component.PlatformMeta{Org: "acme", Platform: "ops"},
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	c := discoverable.(*Component)
	require.NoError(t, c.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, c.Stop(context.Background())) })
	loopID := c.handler.loopManager.GenerateLoopID()
	bucket := &terminalSelectionBucket{&approvalRevisionBucket{
		settlementBucket: &settlementBucket{values: make(map[string][]byte), failPutKey: loopID, failPutLeft: 1},
		revisions:        make(map[string]uint64),
	}}
	c.loopsBucket = bucket
	c.settlementEvidence = &settlementEvidence{}
	lineageCalls := 0
	c.testLineageWriteHook = func(_ context.Context, _ string, _ map[string]any) error {
		lineageCalls++
		return errs.ClassifiedCode(errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired,
			&graph.StateContractError{Reason: graph.GraphStateReasonNoncanonicalEntityID})
	}
	task := validLineageTask("birth-create-redelivery")
	task.LoopID = loopID
	task.Metadata = map[string]any{agentic.MetadataKeyRelatedLoops: map[string]any{"researcher": "upstream-loop"}}
	data := settlementEnvelope(t, &task)

	decision, err := c.handleTaskMessage(t.Context(), data)
	require.ErrorContains(t, err, "injected final marker failure")
	require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
	require.Empty(t, bucket.values, "failed initial Create must not invent durable completion")
	require.Empty(t, perLoopMapCount(c.handler.loopManager, loopID))
	require.Equal(t, 1, lineageCalls)

	decision, err = c.handleTaskMessage(t.Context(), data)
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	require.Equal(t, 2, lineageCalls, "redelivery must retry birth, not ACK active process deduplication")
	var terminal agentic.LoopEntity
	require.NoError(t, json.Unmarshal(bucket.values[loopID], &terminal))
	require.NoError(t, terminal.Validate())
	require.Equal(t, agentic.LoopStateFailed, terminal.State)
	require.Contains(t, bucket.values, "COMPLETE_"+loopID)
	require.Empty(t, perLoopMapCount(c.handler.loopManager, loopID))
}
