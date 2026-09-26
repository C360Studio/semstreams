//go:build integration

package agenticloop

// A cancel racing a non-terminal result on its way to the loop-record carrier
// (#1377 W3/W4, design § 4 T3, T4, T7, T8, T9), through the production lane
// callbacks setupSubscriptions wires, over a real broker.
//
// The interleavings are forced, never sampled. The approval lane and the
// carrier pause on their test-only stage hooks (testApprovedDispatchHook
// "dispatched", testCarrierHook "published"); the cancel lane pauses on the
// loops bucket's Create of COMPLETE_<loopID> (after CancelLoop moved the loop
// cancelled in memory, before the owner's record write), or on the
// active_loops decrement the owner makes after its record write returned and
// before the cancel lane releases the loop (T9: the record write itself runs
// under loopRecordMu, so a pause inside it would hold the carrier out).
//
// These started as the PR #1388 probe, which asserted the defect; they now
// assert the fix and each one's mutation is recorded on the PR.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type raceRecordWrite struct {
	lane     string
	op       string
	state    agentic.LoopState
	revision uint64
	err      string
}

// raceBucket passes every call to the real loops bucket, records each write of
// one loop's record with the lane that made it, and can pause the first Create
// of COMPLETE_<loopID>.
type raceBucket struct {
	jetstream.KeyValue
	loopID        string
	pauseMarker   atomic.Bool
	markerReached chan struct{}
	markerRelease chan struct{}
	mu            sync.Mutex
	writes        []raceRecordWrite
}

func newRaceBucket(kv jetstream.KeyValue, loopID string) *raceBucket {
	return &raceBucket{KeyValue: kv, loopID: loopID,
		markerReached: make(chan struct{}), markerRelease: make(chan struct{})}
}

// laneOf names the lane a write came from by its goroutine's stack.
func laneOf() string {
	buf := make([]byte, 1<<16)
	stack := string(buf[:runtime.Stack(buf, false)])
	switch {
	case strings.Contains(stack, "handleCancelSignal"):
		return "cancel-lane"
	case strings.Contains(stack, "handleApprovalResponseMessage"):
		return "approval-carrier"
	case strings.Contains(stack, "handleToolResultMessage"):
		return "tool-carrier"
	default:
		return "other"
	}
}

func (b *raceBucket) Create(ctx context.Context, key string, value []byte, opts ...jetstream.KVCreateOpt) (uint64, error) {
	if key == terminalMarkerKey(b.loopID) && b.pauseMarker.CompareAndSwap(true, false) {
		close(b.markerReached)
		<-b.markerRelease
	}
	return b.KeyValue.Create(ctx, key, value, opts...)
}

func (b *raceBucket) record(op string, value []byte, rev uint64, err error) {
	var entity agentic.LoopEntity
	_ = json.Unmarshal(value, &entity)
	w := raceRecordWrite{lane: laneOf(), op: op, state: entity.State, revision: rev}
	if err != nil {
		w.err = err.Error()
	}
	b.mu.Lock()
	b.writes = append(b.writes, w)
	b.mu.Unlock()
}

func (b *raceBucket) Update(ctx context.Context, key string, value []byte, revision uint64) (uint64, error) {
	rev, err := b.KeyValue.Update(ctx, key, value, revision)
	if key == b.loopID {
		b.record("Update", value, rev, err)
	}
	return rev, err
}

func (b *raceBucket) Put(ctx context.Context, key string, value []byte) (uint64, error) {
	rev, err := b.KeyValue.Put(ctx, key, value)
	if key == b.loopID {
		b.record("Put", value, rev, err)
	}
	return rev, err
}

func (b *raceBucket) recorded() []raceRecordWrite {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]raceRecordWrite(nil), b.writes...)
}

// stagePause blocks the first call of a test hook at one stage for one loop.
type stagePause struct {
	loopID, stage string
	armed         atomic.Bool
	reached       chan struct{}
	release       chan struct{}
}

func newStagePause(loopID, stage string) *stagePause {
	p := &stagePause{loopID: loopID, stage: stage, reached: make(chan struct{}), release: make(chan struct{})}
	p.armed.Store(true)
	return p
}

func (p *stagePause) hook(loopID, stage string) {
	if loopID == p.loopID && stage == p.stage && p.armed.CompareAndSwap(true, false) {
		close(p.reached)
		<-p.release
	}
}

// pausingGauge pauses the first Dec after it is armed. The terminal owner
// decrements active_loops once its record write has returned and released
// loopRecordMu, and the cancel lane releases the loop only after that, so the
// pause sits exactly between the owner's write and the release (ordering C).
type pausingGauge struct {
	prometheus.Gauge
	armed   atomic.Bool
	reached chan struct{}
	release chan struct{}
}

func (g *pausingGauge) Dec() {
	g.Gauge.Dec()
	if g.armed.CompareAndSwap(true, false) {
		close(g.reached)
		<-g.release
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

type raceLane struct {
	client    *natsclient.Client
	c         *Component
	h         *MessageHandler
	callbacks map[string]func(context.Context, jetstream.Msg)
	handles   map[string]*loopPolicyHandle
	logs      *syncBuffer
}

// startRaceLane builds the component through its production constructor on
// its own broker and wires its lanes through setupSubscriptions.
func startRaceLane(t *testing.T) *raceLane {
	t.Helper()
	return startRaceLaneOn(t, newLoopNATS(t))
}

// startRaceLaneOn is startRaceLane over a broker another process already
// uses: a second process with its own memory over the same durable state.
func startRaceLaneOn(t *testing.T, client *natsclient.Client) *raceLane {
	t.Helper()
	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{
		NATSClient: client, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.started = true
	c.startTime = time.Now()
	c.graphWriter = nil
	logs := &syncBuffer{}
	c.logger = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	c.metrics = getMetrics(metric.NewMetricsRegistry())
	c.handler.SetMetrics(c.metrics)
	require.NoError(t, c.initializeKVBuckets(t.Context()))

	lane := &raceLane{
		client: client, c: c, h: c.handler, logs: logs,
		callbacks: make(map[string]func(context.Context, jetstream.Msg)),
		handles:   make(map[string]*loopPolicyHandle),
	}
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	c.consumeStream = func(_ context.Context, _ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		handle := &loopPolicyHandle{closed: make(chan struct{})}
		lane.callbacks[owner.Port] = callback
		lane.handles[owner.Port] = handle
		return handle, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, c.setupSubscriptions(ctx, ctx))
	t.Cleanup(func() {
		cancel()
		for _, binding := range c.consumers {
			<-binding.Done()
		}
	})
	return lane
}

// toolBatchLoop births a loop and dispatches a one-call batch, as the model
// lane does; it returns the loop and the call now on tool.execute.
func toolBatchLoop(t *testing.T, lane *raceLane, taskID, toolName string) (string, agentic.ToolCall) {
	t.Helper()
	c, h := lane.c, lane.h
	result, err := h.HandleTask(t.Context(), TaskMessage{
		TaskID: taskID, Role: "general", Model: "test-model", Prompt: bornLoopPrompt,
	})
	require.NoError(t, err)
	loopID := result.LoopID
	require.NoError(t, c.createLoopState(t.Context(), loopID))
	require.NoError(t, c.publishResults(t.Context(), result))
	firstRequest := mintedRequest(t, result)

	batch := agentic.AgentResponse{
		RequestID: firstRequest, Status: agentic.StatusToolCall, FinishReason: "tool_calls",
		Message: agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{{ID: "call-" + taskID, Name: toolName}}},
	}
	retainModelResponse(t, lane.client, batch)
	dispatch, err := h.HandleModelResponse(t.Context(), loopID, batch)
	require.NoError(t, err)
	require.NoError(t, c.persistHandlerResult(t.Context(), dispatch))
	call, _ := dispatchedToolCall(t, dispatch)
	return loopID, call
}

// gatedLaneLoop is a loop gated on a human approval, built by the real lanes.
func gatedLaneLoop(t *testing.T, lane *raceLane, taskID string) (string, *agentic.PendingApprovalState) {
	t.Helper()
	loopID, call := toolBatchLoop(t, lane, taskID, "delete_rule")
	_, delivered := deliverToolResult(t, lane.c, agentic.ToolResult{
		CallID: call.ID, Name: call.Name, LoopID: loopID,
		Error:     agentic.ApprovalRequiredPrefix + "confirm the deletion",
		RequestID: call.RequestID, ExecutionID: call.ExecutionID, CallOrdinal: call.CallOrdinal,
	})
	require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision())
	record := loopRecordOf(t, lane.c, loopID)
	require.Equal(t, agentic.LoopStateAwaitingApproval, record.entity.State)
	require.NotNil(t, record.entity.PendingApproval)
	return loopID, record.entity.PendingApproval
}

func cancelSignalBytes(t *testing.T, loopID string) []byte {
	t.Helper()
	return baseMessageBytes(t, &agentic.UserSignal{
		SignalID: "signal-" + loopID, Type: agentic.SignalCancel, LoopID: loopID,
		UserID: "operator", Timestamp: time.Now().UTC(),
	})
}

type raceMetrics struct {
	failed      map[string]float64
	dropped     map[string]float64
	signalsDrop map[string]float64
	active      float64
}

func vecValues(cv *prometheus.CounterVec) map[string]float64 {
	out := map[string]float64{}
	ch := make(chan prometheus.Metric, 128)
	go func() { cv.Collect(ch); close(ch) }()
	for m := range ch {
		var pb dto.Metric
		_ = m.Write(&pb)
		label := ""
		if len(pb.GetLabel()) > 0 {
			label = pb.GetLabel()[0].GetValue()
		}
		out[label] = pb.GetCounter().GetValue()
	}
	return out
}

func snapshotRaceMetrics(c *Component) raceMetrics {
	var g dto.Metric
	_ = c.metrics.activeLoops.Write(&g)
	return raceMetrics{
		failed:      vecValues(c.metrics.loopsFailed),
		dropped:     vecValues(c.metrics.toolResultsDropped),
		signalsDrop: vecValues(c.metrics.signalsDropped),
		active:      g.GetGauge().GetValue(),
	}
}

func deltas(before, after map[string]float64) map[string]float64 {
	out := map[string]float64{}
	for k, v := range after {
		if d := v - before[k]; d != 0 {
			out[k] = d
		}
	}
	return out
}

type raceMetricDelta struct {
	failed, dropped, signalsDrop map[string]float64
	active                       float64
}

func metricDelta(before, after raceMetrics) raceMetricDelta {
	return raceMetricDelta{
		failed:      deltas(before.failed, after.failed),
		dropped:     deltas(before.dropped, after.dropped),
		signalsDrop: deltas(before.signalsDrop, after.signalsDrop),
		active:      after.active - before.active,
	}
}

// approvedToolCallsOn counts tool.execute messages retained for loopID that
// carry the approver's stamp, i.e. the approval's re-dispatch.
func approvedToolCallsOn(t *testing.T, client *natsclient.Client, loopID string) int {
	t.Helper()
	stream, err := client.GetStream(t.Context(), loopStreamName)
	require.NoError(t, err)
	info, err := stream.Info(t.Context())
	require.NoError(t, err)
	n := 0
	for seq := info.State.FirstSeq; seq <= info.State.LastSeq; seq++ {
		raw, err := stream.GetMsg(t.Context(), seq)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(raw.Subject, "tool.execute.") {
			continue
		}
		var envelope struct {
			Payload agentic.ToolCall `json:"payload"`
		}
		require.NoError(t, json.Unmarshal(raw.Data, &envelope))
		if envelope.Payload.LoopID == loopID && envelope.Payload.ApprovedBy == "operator" {
			n++
		}
	}
	return n
}

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(20 * time.Second):
		t.Fatalf("interleaving not reached: %s", what)
	}
}

func dispositionOf(m *loopSettlementMsg) string {
	return strings.Join([]string{
		"acks=" + itoa(m.acks.Load()), "naks=" + itoa(m.naks.Load()), "terms=" + itoa(m.terms.Load()),
	}, " ")
}

func itoa(n int32) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// deliverOn hands data to a lane callback in the background and returns the
// message and a channel closed when the callback returns.
func deliverOn(t *testing.T, lane *raceLane, port string, data []byte) (*loopSettlementMsg, chan struct{}) {
	t.Helper()
	callback := lane.callbacks[port]
	require.NotNil(t, callback, "lane %s is wired", port)
	msg := &loopSettlementMsg{data: data}
	done := make(chan struct{})
	go func() {
		defer close(done)
		callback(t.Context(), msg)
	}()
	return msg, done
}

// deliverHeartbeatOn is deliverOn for a heartbeat lane, which reads the
// delivery's metadata before it runs the work.
func deliverHeartbeatOn(t *testing.T, lane *raceLane, port string, data []byte) (*loopDeliveryOwnerMsg, chan struct{}) {
	t.Helper()
	callback := lane.callbacks[port]
	require.NotNil(t, callback, "lane %s is wired", port)
	msg := &loopDeliveryOwnerMsg{data: data}
	done := make(chan struct{})
	go func() {
		defer close(done)
		callback(t.Context(), msg)
	}()
	return msg, done
}

func heartbeatDisposition(m *loopDeliveryOwnerMsg) string {
	return "acks=" + itoa(m.acks.Load()) + " naks=" + itoa(m.naks.Load()) + " terms=" + itoa(m.terms.Load())
}

func requireLaneNotLatched(t *testing.T, lane *raceLane, port string) {
	t.Helper()
	require.Zero(t, lane.handles[port].drains.Load(), "the %s lane's consumer is not drained", port)
	require.NotEqual(t, "delivery ownership lost", lane.c.Health().Status, "loop health is not latched")
	require.NotContains(t, lane.logs.String(), "Loop delivery refused by latched lane")
}

// requireNextAnswerDispatches is the control every ordering shares: a valid
// answer on another gated loop, on the same lane callback, still dispatches.
func requireNextAnswerDispatches(t *testing.T, lane *raceLane, loopID string, gate *agentic.PendingApprovalState) {
	t.Helper()
	before := approvedToolCallsOn(t, lane.client, loopID)
	msg, done := deliverOn(t, lane, "agent.approval_response", approveAnswer(t, gate, loopID))
	waitFor(t, done, "control approval returned")
	record := loopRecordOf(t, lane.c, loopID)
	require.Equal(t, int32(1), msg.acks.Load(), "control: the valid answer is acknowledged (%s)", dispositionOf(msg))
	require.Equal(t, before+1, approvedToolCallsOn(t, lane.client, loopID), "control: the valid answer dispatches")
	require.NotEqual(t, agentic.LoopStateAwaitingApproval, record.entity.State, "control: the gate is cleared (%s)", record.entity.State)
	require.Nil(t, record.entity.PendingApproval, "control: the gate is cleared")
}

// T3 — W3: the cancel lands after the approval's AddPendingTool and before the
// carrier's entry check, with the cancel's terminal commit still in flight.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestACancelBeforeTheCarriersCheckPublishesAndWritesNothing(t *testing.T) {
	lane := startRaceLane(t)
	loopID, gate := gatedLaneLoop(t, lane, "task-t3")
	controlLoopID, controlGate := gatedLaneLoop(t, lane, "task-t3-control")
	c := lane.c

	bucket := newRaceBucket(c.loopsBucket, loopID)
	c.loopsBucket = bucket
	pause := newStagePause(loopID, "dispatched")
	lane.h.testApprovedDispatchHook = pause.hook
	before := snapshotRaceMetrics(c)
	approvedBefore := approvedToolCallsOn(t, lane.client, loopID)
	gatedRecord := loopRecordOf(t, c, loopID)

	approvalMsg, approvalDone := deliverOn(t, lane, "agent.approval_response", approveAnswer(t, gate, loopID))
	waitFor(t, pause.reached, "approval lane after dispatchToolCall")
	require.Contains(t, lane.h.loopManager.GetPendingTools(loopID), gate.CallID,
		"fixture: AddPendingTool already ran when the approval lane paused")

	bucket.pauseMarker.Store(true)
	signalMsg, signalDone := deliverOn(t, lane, "agent.signal", cancelSignalBytes(t, loopID))
	waitFor(t, bucket.markerReached, "cancel lane at COMPLETE_ create")
	held, err := lane.h.GetLoop(loopID)
	require.NoError(t, err, "fixture: the cancel lane has not released the loop")
	require.Equal(t, agentic.LoopStateCancelled, held.State)

	close(pause.release)
	waitFor(t, approvalDone, "approval callback returned")
	afterApproval := loopRecordOf(t, c, loopID)
	_, markerErr := bucket.KeyValue.Get(t.Context(), terminalMarkerKey(loopID))
	t.Logf("T3 at approval return: %s; record %s rev %d; writes %+v",
		dispositionOf(approvalMsg), afterApproval.entity.State, afterApproval.revision, bucket.recorded())
	assert.Equal(t, int32(1), approvalMsg.naks.Load(), "the live record retries the approval")
	require.Zero(t, approvalMsg.acks.Load()+approvalMsg.terms.Load())
	assert.Equal(t, approvedBefore, approvedToolCallsOn(t, lane.client, loopID),
		"no approved tool.execute is published for a loop cancelled in memory")
	assert.Empty(t, bucket.recorded(), "the carrier writes no record")
	require.Equal(t, gatedRecord.revision, afterApproval.revision)
	require.ErrorIs(t, markerErr, jetstream.ErrKeyNotFound, "fixture: the cancel's marker is not created yet")

	close(bucket.markerRelease)
	waitFor(t, signalDone, "signal callback returned")
	writes := bucket.recorded()
	final := metricDelta(before, snapshotRaceMetrics(c))
	require.Equal(t, int32(1), signalMsg.acks.Load(), "the cancel is acknowledged")
	require.Len(t, writes, 1, "the record has exactly one terminal writer")
	require.Equal(t, "cancel-lane", writes[0].lane)
	require.Equal(t, agentic.LoopStateCancelled, writes[0].state)
	require.Empty(t, writes[0].err)
	require.Equal(t, map[string]float64{"cancelled": 1}, final.failed)
	require.Equal(t, float64(-1), final.active)
	require.Empty(t, final.dropped)

	// The redelivered answer reads the cancelled record through the cold branch.
	redelivered, redeliveredDone := deliverOn(t, lane, "agent.approval_response", approveAnswer(t, gate, loopID))
	waitFor(t, redeliveredDone, "redelivered approval returned")
	afterRedelivery := metricDelta(before, snapshotRaceMetrics(c))
	require.Equal(t, int32(1), redelivered.acks.Load(), "the redelivery is acknowledged (%s)", dispositionOf(redelivered))
	require.Equal(t, map[string]float64{"approval_inapplicable": 1}, afterRedelivery.dropped)
	require.Equal(t, approvedBefore, approvedToolCallsOn(t, lane.client, loopID), "the redelivery publishes nothing")
	require.Len(t, bucket.recorded(), 1, "the redelivery writes nothing")

	requireLaneNotLatched(t, lane, "agent.approval_response")
	requireNextAnswerDispatches(t, lane, controlLoopID, controlGate)
}

// T4 — W4: the cancel commits and releases the loop while the approval is
// mid-dispatch, before the carrier's entry check.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestALoopReleasedBeforeTheCarriersCheckIsSettledByItsRecord(t *testing.T) {
	lane := startRaceLane(t)
	loopID, gate := gatedLaneLoop(t, lane, "task-t4")
	otherLoopID, otherGate := gatedLaneLoop(t, lane, "task-t4-other")
	c := lane.c

	bucket := newRaceBucket(c.loopsBucket, loopID)
	c.loopsBucket = bucket
	pause := newStagePause(loopID, "dispatched")
	lane.h.testApprovedDispatchHook = pause.hook
	before := snapshotRaceMetrics(c)
	approvedBefore := approvedToolCallsOn(t, lane.client, loopID)

	approvalMsg, approvalDone := deliverOn(t, lane, "agent.approval_response", approveAnswer(t, gate, loopID))
	waitFor(t, pause.reached, "approval lane after dispatchToolCall")

	signalMsg, signalDone := deliverOn(t, lane, "agent.signal", cancelSignalBytes(t, loopID))
	waitFor(t, signalDone, "signal callback returned")
	require.Equal(t, int32(1), signalMsg.acks.Load())
	_, heldErr := lane.h.GetLoop(loopID)
	require.ErrorIs(t, heldErr, ErrLoopNotFound, "fixture: the cancel lane released the loop")
	cancelled := loopRecordOf(t, c, loopID)
	require.Equal(t, agentic.LoopStateCancelled, cancelled.entity.State)

	close(pause.release)
	waitFor(t, approvalDone, "approval callback returned")
	after := loopRecordOf(t, c, loopID)
	delta := metricDelta(before, snapshotRaceMetrics(c))
	t.Logf("T4 approval: %s; record %s rev %d; writes %+v; deltas %+v; health %q %q",
		dispositionOf(approvalMsg), after.entity.State, after.revision, bucket.recorded(), delta,
		c.Health().Status, c.Health().LastError)
	assert.Equal(t, int32(1), approvalMsg.acks.Load(), "the approval is acknowledged without effect")
	require.Zero(t, approvalMsg.naks.Load()+approvalMsg.terms.Load())
	requireLaneNotLatched(t, lane, "agent.approval_response")
	assert.Equal(t, approvedBefore, approvedToolCallsOn(t, lane.client, loopID), "nothing is published")
	assert.Equal(t, cancelled.revision, after.revision, "the record's revision is unchanged")
	assert.Len(t, bucket.recorded(), 1, "only the cancel lane wrote the record")
	require.Equal(t, map[string]float64{"terminal_unproven": 1}, delta.dropped)
	require.Equal(t, map[string]float64{"cancelled": 1}, delta.failed)
	require.Equal(t, float64(-1), delta.active)

	requireNextAnswerDispatches(t, lane, otherLoopID, otherGate)
}

// T7 — ordering A: the cancel lands after the carrier's check and published
// the approved call; the carrier's record write comes before the owner's
// marker. The second half is the process dying there: a second process with
// no memory of the loop takes the cancel.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestACancelAfterTheCheckBeforeItsMarkerLeavesTheRecordToItsOwner(t *testing.T) {
	lane := startRaceLane(t)
	loopID, gate := gatedLaneLoop(t, lane, "task-t7")
	c := lane.c

	bucket := newRaceBucket(c.loopsBucket, loopID)
	c.loopsBucket = bucket
	pause := newStagePause(loopID, "published")
	c.testCarrierHook = pause.hook
	before := snapshotRaceMetrics(c)
	approvedBefore := approvedToolCallsOn(t, lane.client, loopID)
	gatedRecord := loopRecordOf(t, c, loopID)

	approvalMsg, approvalDone := deliverOn(t, lane, "agent.approval_response", approveAnswer(t, gate, loopID))
	waitFor(t, pause.reached, "carrier after its publication")
	require.Equal(t, approvedBefore+1, approvedToolCallsOn(t, lane.client, loopID),
		"fixture: the approved call was published before the cancel")

	bucket.pauseMarker.Store(true)
	signalMsg, signalDone := deliverOn(t, lane, "agent.signal", cancelSignalBytes(t, loopID))
	waitFor(t, bucket.markerReached, "cancel lane at COMPLETE_ create")

	close(pause.release)
	waitFor(t, approvalDone, "approval callback returned")
	afterApproval := loopRecordOf(t, c, loopID)
	t.Logf("T7 at approval return: %s; record %s rev %d; writes %+v",
		dispositionOf(approvalMsg), afterApproval.entity.State, afterApproval.revision, bucket.recorded())
	assert.Equal(t, int32(1), approvalMsg.naks.Load(), "the live record retries the approval")
	assert.Zero(t, approvalMsg.acks.Load()+approvalMsg.terms.Load())
	assert.Empty(t, bucket.recorded(), "no carrier write: no cancelled record precedes its marker")
	assert.Equal(t, gatedRecord.revision, afterApproval.revision)
	assert.Equal(t, agentic.LoopStateAwaitingApproval, afterApproval.entity.State, "the record stays live")

	// The process dies here. A second process takes the same cancel: the record
	// is live and no marker exists, so the honest answer is Retry — never
	// stale_loop_id, which would acknowledge a cancellation nobody published.
	second := startRaceLaneOn(t, lane.client)
	secondBefore := snapshotRaceMetrics(second.c)
	secondSignal, secondDone := deliverOn(t, second, "agent.signal", cancelSignalBytes(t, loopID))
	waitFor(t, secondDone, "second process's cancel returned")
	secondDelta := metricDelta(secondBefore, snapshotRaceMetrics(second.c))
	t.Logf("T7 second process cancel: %s; deltas %+v", dispositionOf(secondSignal), secondDelta)
	assert.Equal(t, int32(1), secondSignal.naks.Load(), "the second process retries the cancel")
	require.Zero(t, secondSignal.acks.Load()+secondSignal.terms.Load())
	assert.Empty(t, secondDelta.signalsDrop, "never acknowledged as stale_loop_id")

	close(bucket.markerRelease)
	waitFor(t, signalDone, "signal callback returned")
	writes := bucket.recorded()
	require.Equal(t, int32(1), signalMsg.acks.Load())
	require.Len(t, writes, 1, "the record has exactly one terminal writer")
	require.Equal(t, "cancel-lane", writes[0].lane)
	require.Equal(t, agentic.LoopStateCancelled, writes[0].state)
	_, markerErr := bucket.KeyValue.Get(t.Context(), terminalMarkerKey(loopID))
	require.NoError(t, markerErr, "the marker exists once the owner ran")
	require.Equal(t, uint64(1), messagesOn(t, lane.client, "agent.complete."+loopID))
	final := metricDelta(before, snapshotRaceMetrics(c))
	require.Equal(t, map[string]float64{"cancelled": 1}, final.failed)
	require.Empty(t, final.dropped)
	requireLaneNotLatched(t, lane, "agent.approval_response")
}

// T8 — ordering B: the owner commits and releases the loop between the
// carrier's publication and its record write. The approval arm publishes an
// approved call (mints nothing); the tool arm completes a batch and mints the
// next request, so it meets the release at the stamp, before the render.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestALoopReleasedBetweenPublishAndWriteIsSettledByItsRecord(t *testing.T) {
	t.Run("approval lane", func(t *testing.T) {
		lane := startRaceLane(t)
		loopID, gate := gatedLaneLoop(t, lane, "task-t8")
		otherLoopID, otherGate := gatedLaneLoop(t, lane, "task-t8-other")
		c := lane.c

		bucket := newRaceBucket(c.loopsBucket, loopID)
		c.loopsBucket = bucket
		pause := newStagePause(loopID, "published")
		c.testCarrierHook = pause.hook
		before := snapshotRaceMetrics(c)
		approvedBefore := approvedToolCallsOn(t, lane.client, loopID)

		approvalMsg, approvalDone := deliverOn(t, lane, "agent.approval_response", approveAnswer(t, gate, loopID))
		waitFor(t, pause.reached, "carrier after its publication")

		signalMsg, signalDone := deliverOn(t, lane, "agent.signal", cancelSignalBytes(t, loopID))
		waitFor(t, signalDone, "signal callback returned")
		require.Equal(t, int32(1), signalMsg.acks.Load())
		_, heldErr := lane.h.GetLoop(loopID)
		require.ErrorIs(t, heldErr, ErrLoopNotFound, "fixture: the cancel lane released the loop")
		cancelled := loopRecordOf(t, c, loopID)

		close(pause.release)
		waitFor(t, approvalDone, "approval callback returned")
		after := loopRecordOf(t, c, loopID)
		delta := metricDelta(before, snapshotRaceMetrics(c))
		t.Logf("T8 approval: %s; record %s rev %d; writes %+v; deltas %+v; health %q %q",
			dispositionOf(approvalMsg), after.entity.State, after.revision, bucket.recorded(), delta,
			c.Health().Status, c.Health().LastError)
		assert.Equal(t, int32(1), approvalMsg.acks.Load(), "no Fatal: the record settles the delivery")
		require.Zero(t, approvalMsg.naks.Load()+approvalMsg.terms.Load())
		requireLaneNotLatched(t, lane, "agent.approval_response")
		assert.Equal(t, approvedBefore+1, approvedToolCallsOn(t, lane.client, loopID),
			"the one publication let out before the release")
		require.Equal(t, cancelled.revision, after.revision)
		assert.Len(t, bucket.recorded(), 1, "only the cancel lane wrote the record")
		require.Equal(t, map[string]float64{"terminal_unproven": 1}, delta.dropped)

		// The published call's result arrives for the released, cancelled loop
		// and is acknowledged without effect.
		executed := agentic.ToolResult{
			CallID: gate.CallID, Name: gate.ToolName, Content: "the approved call ran", LoopID: loopID,
			RequestID: gate.RequestID, ExecutionID: gate.ExecutionID, CallOrdinal: gate.CallOrdinal,
		}
		droppedBefore := snapshotRaceMetrics(c)
		_, delivered := deliverToolResult(t, c, executed)
		resultDelta := metricDelta(droppedBefore, snapshotRaceMetrics(c))
		t.Logf("T8 executed result: %v; deltas %+v", delivered.Decision(), resultDelta)
		require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision())
		// The loop is released, so the result takes the tool lane's cold arm,
		// which reads the terminal record and counts the drop as a result with
		// no live execution (stale_execution); a loop still held would count it
		// terminal_unproven on the warm arm.
		require.Equal(t, map[string]float64{"stale_execution": 1}, resultDelta.dropped)
		require.Empty(t, resultDelta.failed)
		require.Len(t, bucket.recorded(), 1, "the result writes nothing")

		requireNextAnswerDispatches(t, lane, otherLoopID, otherGate)
	})

	t.Run("tool lane: a batch-completing result meets the release at the stamp", func(t *testing.T) {
		lane := startRaceLane(t)
		loopID, call := toolBatchLoop(t, lane, "task-t8-tool", "search")
		controlLoopID, controlCall := toolBatchLoop(t, lane, "task-t8-tool-control", "search")
		c := lane.c

		bucket := newRaceBucket(c.loopsBucket, loopID)
		c.loopsBucket = bucket
		pause := newStagePause(loopID, "published")
		c.testCarrierHook = pause.hook
		before := snapshotRaceMetrics(c)
		requestSubject := "agent.request." + loopID
		require.Equal(t, uint64(1), messagesOn(t, lane.client, requestSubject))

		resultMsg, resultDone := deliverHeartbeatOn(t, lane, "tool.result", baseMessageBytes(t, &agentic.ToolResult{
			CallID: call.ID, Name: call.Name, Content: "searched", LoopID: loopID,
			RequestID: call.RequestID, ExecutionID: call.ExecutionID, CallOrdinal: call.CallOrdinal,
		}))
		waitFor(t, pause.reached, "carrier after publishing the next request")
		require.Equal(t, uint64(2), messagesOn(t, lane.client, requestSubject),
			"fixture: the completed batch minted and published the next request")

		signalMsg, signalDone := deliverOn(t, lane, "agent.signal", cancelSignalBytes(t, loopID))
		waitFor(t, signalDone, "signal callback returned")
		require.Equal(t, int32(1), signalMsg.acks.Load())
		cancelled := loopRecordOf(t, c, loopID)
		require.Equal(t, agentic.LoopStateCancelled, cancelled.entity.State)

		close(pause.release)
		waitFor(t, resultDone, "tool result callback returned")
		after := loopRecordOf(t, c, loopID)
		delta := metricDelta(before, snapshotRaceMetrics(c))
		t.Logf("T8 tool: %s; record %s rev %d published %s; writes %+v; deltas %+v; health %q %q",
			heartbeatDisposition(resultMsg), after.entity.State, after.revision, after.entity.PublishedRequestID,
			bucket.recorded(), delta, c.Health().Status, c.Health().LastError)
		assert.Equal(t, int32(1), resultMsg.acks.Load(), "no Fatal: the stamp's not-found is the release")
		require.Zero(t, resultMsg.naks.Load()+resultMsg.terms.Load())
		requireLaneNotLatched(t, lane, "tool.result")
		require.Equal(t, uint64(2), messagesOn(t, lane.client, requestSubject), "the minted request is retained once")
		require.Equal(t, call.RequestID, after.entity.PublishedRequestID, "and named by no record")
		require.Equal(t, cancelled.revision, after.revision)
		assert.Len(t, bucket.recorded(), 1, "only the cancel lane wrote the record")
		require.Equal(t, map[string]float64{"terminal_unproven": 1}, delta.dropped)

		// Control: the same result shape on a held loop mints, stamps and writes.
		controlMsg, controlDone := deliverHeartbeatOn(t, lane, "tool.result", baseMessageBytes(t, &agentic.ToolResult{
			CallID: controlCall.ID, Name: controlCall.Name, Content: "searched", LoopID: controlLoopID,
			RequestID: controlCall.RequestID, ExecutionID: controlCall.ExecutionID, CallOrdinal: controlCall.CallOrdinal,
		}))
		waitFor(t, controlDone, "control tool result returned")
		controlRecord := loopRecordOf(t, c, controlLoopID)
		require.Equal(t, int32(1), controlMsg.acks.Load(), "control: %s", heartbeatDisposition(controlMsg))
		require.Equal(t, uint64(2), messagesOn(t, lane.client, "agent.request."+controlLoopID))
		require.NotEqual(t, controlCall.RequestID, controlRecord.entity.PublishedRequestID,
			"control: the record names the request the batch minted")
		require.Equal(t, 1, controlRecord.entity.Iterations)
	})
}

// T9 — ordering C: the owner has written the cancelled record and not yet
// released the loop when the carrier renders its write.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestACarrierWriteAfterTheOwnersRecordIsRefused(t *testing.T) {
	lane := startRaceLane(t)
	loopID, gate := gatedLaneLoop(t, lane, "task-t9")
	c := lane.c

	bucket := newRaceBucket(c.loopsBucket, loopID)
	c.loopsBucket = bucket
	pause := newStagePause(loopID, "published")
	c.testCarrierHook = pause.hook
	gauge := &pausingGauge{Gauge: c.metrics.activeLoops, reached: make(chan struct{}), release: make(chan struct{})}
	// The loop metrics are a process-wide singleton (getMetrics), so the
	// pausing gauge goes on a shallow per-component copy, never on the
	// singleton; the copy's collectors are the singleton's, so every count
	// below is still the one the process exports. The component is per-test,
	// so the copy is never swapped back: a restore in t.Cleanup would run
	// before the component stops and race any stop path that reads c.metrics.
	local := *c.metrics
	local.activeLoops = gauge
	c.metrics = &local
	before := snapshotRaceMetrics(c)

	approvalMsg, approvalDone := deliverOn(t, lane, "agent.approval_response", approveAnswer(t, gate, loopID))
	waitFor(t, pause.reached, "carrier after its publication")

	gauge.armed.Store(true)
	signalMsg, signalDone := deliverOn(t, lane, "agent.signal", cancelSignalBytes(t, loopID))
	waitFor(t, gauge.reached, "cancel lane after its record write, before its release")
	held, err := lane.h.GetLoop(loopID)
	require.NoError(t, err, "fixture: the loop is still held")
	require.Equal(t, agentic.LoopStateCancelled, held.State)
	ownerWrote := loopRecordOf(t, c, loopID)
	require.Equal(t, agentic.LoopStateCancelled, ownerWrote.entity.State, "fixture: the owner's record is written")

	close(pause.release)
	waitFor(t, approvalDone, "approval callback returned")
	writes := bucket.recorded()
	after := loopRecordOf(t, c, loopID)
	t.Logf("T9 approval: %s; record %s rev %d; writes %+v",
		dispositionOf(approvalMsg), after.entity.State, after.revision, writes)
	require.Equal(t, int32(1), approvalMsg.acks.Load(), "the terminal record settles the delivery")
	require.Zero(t, approvalMsg.naks.Load()+approvalMsg.terms.Load())
	require.Len(t, writes, 1, "one terminal write, the owner's")
	require.Equal(t, "cancel-lane", writes[0].lane)
	require.Equal(t, ownerWrote.revision, after.revision)

	close(gauge.release)
	waitFor(t, signalDone, "signal callback returned")
	require.Equal(t, int32(1), signalMsg.acks.Load())
	require.Len(t, bucket.recorded(), 1)
	delta := metricDelta(before, snapshotRaceMetrics(c))
	require.Equal(t, map[string]float64{"terminal_unproven": 1}, delta.dropped)
	require.Equal(t, map[string]float64{"cancelled": 1}, delta.failed)
	requireLaneNotLatched(t, lane, "agent.approval_response")
}

// The pre-AddPendingTool release (design OQ4, § 10 item 7): the cancel commits
// and releases the loop after the approval resolved its gate and before the
// approved call is registered. AddPendingTool refuses the released loop with
// an unclassified error, so the approval is retried; the redelivery takes the
// cold branch and the cancelled record acknowledges it as inapplicable. The
// first disposition is block 1's "the approval lane settles by class ...
// anything else is retried" (scenario "A non-terminal result with an error
// settles on its lane's own disposition").
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestALoopReleasedBeforeTheApprovedCallIsRegisteredIsRetriedThenInapplicable(t *testing.T) {
	lane := startRaceLane(t)
	loopID, gate := gatedLaneLoop(t, lane, "task-pre-add")
	c := lane.c

	bucket := newRaceBucket(c.loopsBucket, loopID)
	c.loopsBucket = bucket
	pause := newStagePause(loopID, "before_dispatch")
	lane.h.testApprovedDispatchHook = pause.hook
	before := snapshotRaceMetrics(c)
	approvedBefore := approvedToolCallsOn(t, lane.client, loopID)

	approvalMsg, approvalDone := deliverOn(t, lane, "agent.approval_response", approveAnswer(t, gate, loopID))
	waitFor(t, pause.reached, "approval lane before the decision switch")
	require.Empty(t, lane.h.loopManager.GetPendingTools(loopID), "fixture: the approved call is not registered yet")

	signalMsg, signalDone := deliverOn(t, lane, "agent.signal", cancelSignalBytes(t, loopID))
	waitFor(t, signalDone, "signal callback returned")
	require.Equal(t, int32(1), signalMsg.acks.Load())
	cancelled := loopRecordOf(t, c, loopID)
	require.Equal(t, agentic.LoopStateCancelled, cancelled.entity.State)

	close(pause.release)
	waitFor(t, approvalDone, "approval callback returned")
	require.Equal(t, int32(1), approvalMsg.naks.Load(), "AddPendingTool's refusal is retried (%s)", dispositionOf(approvalMsg))
	require.Zero(t, approvalMsg.acks.Load()+approvalMsg.terms.Load())
	require.Equal(t, approvedBefore, approvedToolCallsOn(t, lane.client, loopID), "nothing is published")
	require.Equal(t, cancelled.revision, loopRecordOf(t, c, loopID).revision, "nothing is written")

	redelivered, redeliveredDone := deliverOn(t, lane, "agent.approval_response", approveAnswer(t, gate, loopID))
	waitFor(t, redeliveredDone, "redelivered approval returned")
	delta := metricDelta(before, snapshotRaceMetrics(c))
	require.Equal(t, int32(1), redelivered.acks.Load(), "the redelivery is acknowledged (%s)", dispositionOf(redelivered))
	require.Equal(t, map[string]float64{"approval_inapplicable": 1}, delta.dropped)
	require.Len(t, bucket.recorded(), 1, "only the cancel lane wrote the record")
	requireLaneNotLatched(t, lane, "agent.approval_response")
}

// Block 1's "An approval answer whose loop was released after its gate
// resolved is recovered cold", on the lane (#1377 task 2.3, review 1 MEDIUM 2):
// a REJECT resolves its gate, the cancel commits and releases the loop, and
// the reject then reaches HandleToolResult's GetLoop. GetLoop's not-found is
// ErrLoopNotFound, so this FIRST delivery takes the cold branch: the record is
// cancelled, and the answer is acknowledged as inapplicable — never retried.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestARejectWhoseLoopWasReleasedAfterItsGateResolvedIsSettledColdOnItsFirstDelivery(t *testing.T) {
	lane := startRaceLane(t)
	loopID, gate := gatedLaneLoop(t, lane, "task-reject-released")
	c := lane.c

	bucket := newRaceBucket(c.loopsBucket, loopID)
	c.loopsBucket = bucket
	pause := newStagePause(loopID, "before_dispatch")
	lane.h.testApprovedDispatchHook = pause.hook
	before := snapshotRaceMetrics(c)
	requestsBefore := messagesOn(t, lane.client, "agent.request."+loopID)

	reject := baseMessageBytes(t, &agentic.ApprovalResponse{
		LoopID: loopID, CallID: gate.CallID, ExecutionID: gate.ExecutionID, RequestID: gate.RequestID,
		Decision: agentic.ApprovalDecisionReject, ApprovedBy: "operator", Reason: "not this rule",
	})
	rejectMsg, rejectDone := deliverOn(t, lane, "agent.approval_response", reject)
	waitFor(t, pause.reached, "approval lane after the resolve, before the decision switch")

	signalMsg, signalDone := deliverOn(t, lane, "agent.signal", cancelSignalBytes(t, loopID))
	waitFor(t, signalDone, "signal callback returned")
	require.Equal(t, int32(1), signalMsg.acks.Load())
	// require.Error, not ErrorIs: the fixture is "released", and the test is
	// what the lane does with GetLoop's error shape.
	_, heldErr := lane.h.GetLoop(loopID)
	require.Error(t, heldErr, "fixture: the cancel lane released the loop")
	cancelled := loopRecordOf(t, c, loopID)
	require.Equal(t, agentic.LoopStateCancelled, cancelled.entity.State)

	close(pause.release)
	waitFor(t, rejectDone, "approval callback returned")
	delta := metricDelta(before, snapshotRaceMetrics(c))
	t.Logf("released reject: %s; deltas %+v", dispositionOf(rejectMsg), delta)
	assert.Equal(t, int32(1), rejectMsg.acks.Load(), "the first delivery is settled cold, not retried")
	assert.Zero(t, rejectMsg.naks.Load()+rejectMsg.terms.Load())
	assert.Equal(t, map[string]float64{"approval_inapplicable": 1}, delta.dropped)
	require.Equal(t, requestsBefore, messagesOn(t, lane.client, "agent.request."+loopID), "nothing is published")
	require.Equal(t, cancelled.revision, loopRecordOf(t, c, loopID).revision, "nothing is written")
	require.Len(t, bucket.recorded(), 1, "only the cancel lane wrote the record")
	requireLaneNotLatched(t, lane, "agent.approval_response")
}

// The sub-window between the carrier's check and its publication (block 2:
// "A terminal that lands between the carrier's check and its publication lets
// that one publication out"; review 1 MEDIUM 3): the approval passes the entry
// check on a live loop, the cancel moves it cancelled in memory and pauses
// before its marker, and the carrier then publishes the approved call — the
// one publication let out — and its record write is refused.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestACancelBetweenTheCarriersCheckAndItsPublicationLetsOnePublicationOut(t *testing.T) {
	lane := startRaceLane(t)
	loopID, gate := gatedLaneLoop(t, lane, "task-checked")
	c := lane.c

	bucket := newRaceBucket(c.loopsBucket, loopID)
	c.loopsBucket = bucket
	pause := newStagePause(loopID, "checked")
	c.testCarrierHook = pause.hook
	approvedBefore := approvedToolCallsOn(t, lane.client, loopID)
	gatedRecord := loopRecordOf(t, c, loopID)

	approvalMsg, approvalDone := deliverOn(t, lane, "agent.approval_response", approveAnswer(t, gate, loopID))
	waitFor(t, pause.reached, "carrier after its entry check")
	require.Equal(t, approvedBefore, approvedToolCallsOn(t, lane.client, loopID),
		"fixture: nothing is published before the check passes")

	bucket.pauseMarker.Store(true)
	signalMsg, signalDone := deliverOn(t, lane, "agent.signal", cancelSignalBytes(t, loopID))
	waitFor(t, bucket.markerReached, "cancel lane at COMPLETE_ create")

	close(pause.release)
	waitFor(t, approvalDone, "approval callback returned")
	afterApproval := loopRecordOf(t, c, loopID)
	t.Logf("checked: %s; record %s rev %d; writes %+v",
		dispositionOf(approvalMsg), afterApproval.entity.State, afterApproval.revision, bucket.recorded())
	assert.Equal(t, approvedBefore+1, approvedToolCallsOn(t, lane.client, loopID),
		"the one publication in the sub-window is let out")
	assert.Empty(t, bucket.recorded(), "no carrier write")
	assert.Equal(t, int32(1), approvalMsg.naks.Load(), "the live record retries the approval")
	assert.Zero(t, approvalMsg.acks.Load()+approvalMsg.terms.Load())
	assert.Equal(t, gatedRecord.revision, afterApproval.revision, "the record stays as the gate left it")

	close(bucket.markerRelease)
	waitFor(t, signalDone, "signal callback returned")
	writes := bucket.recorded()
	require.Equal(t, int32(1), signalMsg.acks.Load())
	require.Len(t, writes, 1, "the record has exactly one terminal writer")
	require.Equal(t, "cancel-lane", writes[0].lane)
	require.Equal(t, agentic.LoopStateCancelled, writes[0].state)
	requireLaneNotLatched(t, lane, "agent.approval_response")
}
