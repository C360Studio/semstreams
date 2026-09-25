//go:build integration

package agenticloop

// PROBE (#1377, PR #1388): observes the W3 and W4 windows recorded on #1377
// issuecomment-5833255119 through the production approval and signal lane
// callbacks that setupSubscriptions wires, over a real broker. It asserts the
// CURRENT behaviour so a run shows the interleaving is forced, not lucky.
//
// No production hook. The two pauses are existing seams:
//   - the approval lane is paused on the handler logger's Warn that
//     resolveRunEntityID emits for a loop with a RunID in a component with no
//     platform identity (handlers.go:632), reached from dispatchToolCall
//     (handlers.go:2065-2067) — AFTER AddPendingTool (handlers.go:2036) and
//     before the carrier's publish-then-write (component.go:2259).
//   - the cancel lane is paused on the loops bucket's Create of
//     COMPLETE_<loopID> (terminal_owner.go:274), AFTER CancelLoop moved the
//     loop cancelled in memory and before the terminal owner's record write.
// There is no seam between GetLoop (approval_response_handler.go:83) and
// AddPendingTool: the literal pre-AddPendingTool point is not forced here.

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
	"github.com/stretchr/testify/require"
)

const probeRunEntityWarn = "resolveRunEntityID: platform identity missing, RunEntityID will be empty"

// pauseOnLog blocks the first goroutine that logs msg after it is armed.
type pauseOnLog struct {
	msg     string
	armed   atomic.Bool
	reached chan struct{}
	release chan struct{}
	stack   atomic.Value // string: the paused goroutine's stack
}

func newPauseOnLog(msg string) *pauseOnLog {
	return &pauseOnLog{msg: msg, reached: make(chan struct{}), release: make(chan struct{})}
}

func (*pauseOnLog) Enabled(context.Context, slog.Level) bool { return true }
func (p *pauseOnLog) Handle(_ context.Context, r slog.Record) error {
	if r.Message == p.msg && p.armed.CompareAndSwap(true, false) {
		buf := make([]byte, 1<<16)
		p.stack.Store(string(buf[:runtime.Stack(buf, false)]))
		close(p.reached)
		<-p.release
	}
	return nil
}
func (p *pauseOnLog) WithAttrs([]slog.Attr) slog.Handler { return p }
func (p *pauseOnLog) WithGroup(string) slog.Handler      { return p }

type probeRecordWrite struct {
	lane     string
	op       string
	state    agentic.LoopState
	revision uint64
	err      string
}

// raceProbeBucket passes every call to the real loops bucket, records each
// write of the probed loop's record with the lane that made it, and can pause
// the first Create of COMPLETE_<loopID>.
type raceProbeBucket struct {
	jetstream.KeyValue
	loopID        string
	pauseMarker   atomic.Bool
	markerReached chan struct{}
	markerRelease chan struct{}
	mu            sync.Mutex
	writes        []probeRecordWrite
}

func laneOf() string {
	buf := make([]byte, 1<<16)
	stack := string(buf[:runtime.Stack(buf, false)])
	switch {
	case strings.Contains(stack, "handleCancelSignal"):
		return "cancel-lane"
	case strings.Contains(stack, "handleApprovalResponseMessage"):
		return "approval-carrier"
	default:
		return "other"
	}
}

func (b *raceProbeBucket) Create(ctx context.Context, key string, value []byte, opts ...jetstream.KVCreateOpt) (uint64, error) {
	if key == terminalMarkerKey(b.loopID) && b.pauseMarker.CompareAndSwap(true, false) {
		close(b.markerReached)
		<-b.markerRelease
	}
	return b.KeyValue.Create(ctx, key, value, opts...)
}

func (b *raceProbeBucket) Update(ctx context.Context, key string, value []byte, revision uint64) (uint64, error) {
	rev, err := b.KeyValue.Update(ctx, key, value, revision)
	if key == b.loopID {
		var entity agentic.LoopEntity
		_ = json.Unmarshal(value, &entity)
		w := probeRecordWrite{lane: laneOf(), op: "Update", state: entity.State, revision: rev}
		if err != nil {
			w.err = err.Error()
		}
		b.mu.Lock()
		b.writes = append(b.writes, w)
		b.mu.Unlock()
	}
	return rev, err
}

func (b *raceProbeBucket) Put(ctx context.Context, key string, value []byte) (uint64, error) {
	rev, err := b.KeyValue.Put(ctx, key, value)
	if key == b.loopID {
		var entity agentic.LoopEntity
		_ = json.Unmarshal(value, &entity)
		w := probeRecordWrite{lane: laneOf(), op: "Put", state: entity.State, revision: rev}
		if err != nil {
			w.err = err.Error()
		}
		b.mu.Lock()
		b.writes = append(b.writes, w)
		b.mu.Unlock()
	}
	return rev, err
}

func (b *raceProbeBucket) recorded() []probeRecordWrite {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]probeRecordWrite(nil), b.writes...)
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

type probeLane struct {
	client    *natsclient.Client
	c         *Component
	h         *MessageHandler
	callbacks map[string]func(context.Context, jetstream.Msg)
	handles   map[string]*loopPolicyHandle
	logs      *syncBuffer
}

// startProbeLane builds the component through its production constructor, with
// no platform identity, and wires its lanes through setupSubscriptions.
func startProbeLane(t *testing.T) *probeLane {
	t.Helper()
	client := newLoopNATS(t)
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

	lane := &probeLane{
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

// gatedRunLoop is gatedLoop for a loop that belongs to a run, which is what
// makes dispatchToolCall reach resolveRunEntityID.
func gatedRunLoop(t *testing.T, lane *probeLane, taskID string) (string, *agentic.PendingApprovalState) {
	t.Helper()
	c, h := lane.c, lane.h
	result, err := h.HandleTask(t.Context(), TaskMessage{
		TaskID: taskID, Role: "general", Model: "test-model", Prompt: bornLoopPrompt, RunID: "run-" + taskID,
	})
	require.NoError(t, err)
	loopID := result.LoopID
	require.NoError(t, c.createLoopState(t.Context(), loopID))
	require.NoError(t, c.publishResults(t.Context(), result))
	firstRequest := mintedRequest(t, result)

	batch := agentic.AgentResponse{
		RequestID: firstRequest, Status: agentic.StatusToolCall, FinishReason: "tool_calls",
		Message: agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{{ID: "call-gated", Name: "delete_rule"}}},
	}
	retainModelResponse(t, lane.client, batch)
	dispatch, err := h.HandleModelResponse(t.Context(), loopID, batch)
	require.NoError(t, err)
	require.NoError(t, c.persistHandlerResult(t.Context(), dispatch))
	call, _ := dispatchedToolCall(t, dispatch)
	_, delivered := deliverToolResult(t, c, agentic.ToolResult{
		CallID: call.ID, Name: call.Name, LoopID: loopID,
		Error:     agentic.ApprovalRequiredPrefix + "confirm the deletion",
		RequestID: call.RequestID, ExecutionID: call.ExecutionID, CallOrdinal: call.CallOrdinal,
	})
	require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision())
	record := loopRecordOf(t, c, loopID)
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

type probeMetrics struct {
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

func snapshotProbeMetrics(c *Component) probeMetrics {
	var g dto.Metric
	_ = c.metrics.activeLoops.Write(&g)
	return probeMetrics{
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

type probeMetricDelta struct {
	failed, dropped, signalsDrop map[string]float64
	active                       float64
}

func metricDelta(before, after probeMetrics) probeMetricDelta {
	return probeMetricDelta{
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
		t.Fatalf("probe interleaving not reached: %s", what)
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

// W3: the cancel lands while the loop is still held, after the approval's
// AddPendingTool; the cancel lane is held before its terminal marker so the
// approval carrier writes the record first.
func TestProbeW3HeldLoopCancelDuringApprovalDispatch(t *testing.T) {
	lane := startProbeLane(t)
	loopID, gate := gatedRunLoop(t, lane, "task-w3")
	c := lane.c

	bucket := &raceProbeBucket{KeyValue: c.loopsBucket, loopID: loopID,
		markerReached: make(chan struct{}), markerRelease: make(chan struct{})}
	c.loopsBucket = bucket
	pause := newPauseOnLog(probeRunEntityWarn)
	lane.h.logger = slog.New(pause)
	before := snapshotProbeMetrics(c)
	approvedBefore := approvedToolCallsOn(t, lane.client, loopID)

	approvalMsg := &loopSettlementMsg{data: approveAnswer(t, gate, loopID)}
	approvalDone := make(chan struct{})
	pause.armed.Store(true)
	go func() {
		defer close(approvalDone)
		lane.callbacks["agent.approval_response"](t.Context(), approvalMsg)
	}()
	waitFor(t, pause.reached, "approval lane at dispatchToolCall")
	pausedStack, _ := pause.stack.Load().(string)
	require.Contains(t, pausedStack, "dispatchToolCall", "the approval lane is paused inside dispatchToolCall")
	require.Contains(t, lane.h.loopManager.GetPendingTools(loopID), gate.CallID,
		"AddPendingTool already ran when the approval lane paused")

	signalMsg := &loopSettlementMsg{data: cancelSignalBytes(t, loopID)}
	signalDone := make(chan struct{})
	bucket.pauseMarker.Store(true)
	go func() {
		defer close(signalDone)
		lane.callbacks["agent.signal"](t.Context(), signalMsg)
	}()
	waitFor(t, bucket.markerReached, "cancel lane at COMPLETE_ create")
	held, err := lane.h.GetLoop(loopID)
	require.NoError(t, err, "the cancel lane has not released the loop yet")
	require.Equal(t, agentic.LoopStateCancelled, held.State)

	close(pause.release)
	waitFor(t, approvalDone, "approval callback returned")
	afterApproval := loopRecordOf(t, c, loopID)
	approvedAfter := approvedToolCallsOn(t, lane.client, loopID)
	writesAfterApproval := bucket.recorded()
	metricsAfterApproval := metricDelta(before, snapshotProbeMetrics(c))
	_, markerErr := bucket.KeyValue.Get(t.Context(), terminalMarkerKey(loopID))
	completeEvents := messagesOn(t, lane.client, "agent.complete."+loopID)

	// Observed with the cancel lane still held before its marker.
	t.Logf("W3 at approval return: approval %s; approved tool.execute %d->%d; record %s rev %d; writes %+v; "+
		"COMPLETE_ get err=%v; agent.complete retained=%d; metric deltas %+v",
		dispositionOf(approvalMsg), approvedBefore, approvedAfter, afterApproval.entity.State, afterApproval.revision,
		writesAfterApproval, markerErr, completeEvents, metricsAfterApproval)
	require.Equal(t, int32(1), approvalMsg.acks.Load(), "the approval delivery is acknowledged")
	require.Zero(t, approvalMsg.naks.Load()+approvalMsg.terms.Load())
	require.Equal(t, approvedBefore+1, approvedAfter,
		"OBSERVED W3: the approval re-dispatch is published on tool.execute for a loop cancelled in memory")
	require.Equal(t, []probeRecordWrite{{lane: "approval-carrier", op: "Update", state: agentic.LoopStateCancelled,
		revision: afterApproval.revision}}, writesAfterApproval,
		"OBSERVED W3: the approval carrier compare-and-swaps a cancelled record outside the terminal owner")
	require.Equal(t, agentic.LoopStateCancelled, afterApproval.entity.State)
	require.ErrorIs(t, markerErr, jetstream.ErrKeyNotFound,
		"OBSERVED W3: the cancelled record is durable before COMPLETE_<loopID> exists")
	require.Zero(t, completeEvents, "and before any agent.complete event")
	require.Empty(t, metricsAfterApproval.failed)
	require.Empty(t, metricsAfterApproval.dropped)
	require.Zero(t, metricsAfterApproval.active)

	close(bucket.markerRelease)
	waitFor(t, signalDone, "signal callback returned")
	final := loopRecordOf(t, c, loopID)
	writes := bucket.recorded()
	metricsFinal := metricDelta(before, snapshotProbeMetrics(c))

	t.Logf("W3 final: signal %s; record %s rev %d; writes %+v; metric deltas %+v; drains approval=%d signal=%d",
		dispositionOf(signalMsg), final.entity.State, final.revision, writes, metricsFinal,
		lane.handles["agent.approval_response"].drains.Load(), lane.handles["agent.signal"].drains.Load())
	require.Equal(t, int32(1), signalMsg.acks.Load(), "the cancel is acknowledged")
	require.Zero(t, signalMsg.naks.Load()+signalMsg.terms.Load())
	require.Len(t, writes, 2)
	require.Equal(t, "cancel-lane", writes[1].lane, "the cancel lane's terminal owner writes the record last")
	require.Equal(t, agentic.LoopStateCancelled, writes[1].state)
	require.Empty(t, writes[1].err)
	require.Equal(t, map[string]float64{"cancelled": 1}, metricsFinal.failed)
	require.Equal(t, float64(-1), metricsFinal.active)
	require.Empty(t, metricsFinal.dropped, "no tool_results_dropped_total for the dispatched call")
	require.Empty(t, metricsFinal.signalsDrop)
	require.Zero(t, lane.handles["agent.approval_response"].drains.Load())
	require.Zero(t, lane.handles["agent.signal"].drains.Load())
}

// W4: the cancel lane runs to completion and releases the loop while the
// approval lane is paused after AddPendingTool; the carrier's record render
// then cannot read the loop. Then a second, valid approval on another loop is
// handed to the same lane callback.
func TestProbeW4ReleasedLoopQuarantinesApprovalLane(t *testing.T) {
	lane := startProbeLane(t)
	loopID, gate := gatedRunLoop(t, lane, "task-w4")
	otherLoopID, otherGate := gatedRunLoop(t, lane, "task-w4-other")
	c := lane.c

	bucket := &raceProbeBucket{KeyValue: c.loopsBucket, loopID: loopID,
		markerReached: make(chan struct{}), markerRelease: make(chan struct{})}
	c.loopsBucket = bucket
	pause := newPauseOnLog(probeRunEntityWarn)
	lane.h.logger = slog.New(pause)
	before := snapshotProbeMetrics(c)
	approvedBefore := approvedToolCallsOn(t, lane.client, loopID)

	approvalMsg := &loopSettlementMsg{data: approveAnswer(t, gate, loopID)}
	approvalDone := make(chan struct{})
	pause.armed.Store(true)
	go func() {
		defer close(approvalDone)
		lane.callbacks["agent.approval_response"](t.Context(), approvalMsg)
	}()
	waitFor(t, pause.reached, "approval lane at dispatchToolCall")
	pausedStack, _ := pause.stack.Load().(string)
	require.Contains(t, pausedStack, "dispatchToolCall")

	signalMsg := &loopSettlementMsg{data: cancelSignalBytes(t, loopID)}
	lane.callbacks["agent.signal"](t.Context(), signalMsg)
	_, heldErr := lane.h.GetLoop(loopID)
	require.Error(t, heldErr, "fixture: the cancel lane released the loop while the approval lane is paused")
	require.Equal(t, int32(1), signalMsg.acks.Load())
	cancelledRecord := loopRecordOf(t, c, loopID)
	require.Equal(t, agentic.LoopStateCancelled, cancelledRecord.entity.State)

	close(pause.release)
	waitFor(t, approvalDone, "approval callback returned")
	afterApproval := loopRecordOf(t, c, loopID)
	approvedAfter := approvedToolCallsOn(t, lane.client, loopID)
	metricsAfterApproval := metricDelta(before, snapshotProbeMetrics(c))
	health := c.Health()

	t.Logf("W4 approval: %s; approved tool.execute %d->%d; record %s rev %d; writes %+v; metric deltas %+v; "+
		"drains=%d; health status=%q last_error=%q",
		dispositionOf(approvalMsg), approvedBefore, approvedAfter, afterApproval.entity.State, afterApproval.revision,
		bucket.recorded(), metricsAfterApproval, lane.handles["agent.approval_response"].drains.Load(),
		health.Status, health.LastError)
	require.Zero(t, approvalMsg.acks.Load()+approvalMsg.naks.Load()+approvalMsg.terms.Load(),
		"OBSERVED W4: the approval delivery is quarantined (no terminal method)")
	require.Equal(t, int32(1), lane.handles["agent.approval_response"].drains.Load(),
		"OBSERVED W4: the approval lane's consumer is drained")
	require.Equal(t, "delivery ownership lost", health.Status)
	require.Equal(t, "approval result for loop \""+loopID+"\" has unknown durable state: "+
		"agentic-loop.persistHandlerResult: handler result state has unknown durability after its results were published "+
		"failed: get loop "+loopID+" for persistence: LoopManager.GetLoop: find loop failed: loop "+loopID+" not found",
		health.LastError, "OBSERVED W4: the Fatal cause")
	require.Equal(t, approvedBefore+1, approvedAfter,
		"OBSERVED W4: tool.execute is published after the cancelled record is durable")
	require.Equal(t, cancelledRecord.revision, afterApproval.revision, "the approval lane wrote no record")
	require.Equal(t, map[string]float64{"cancelled": 1}, metricsAfterApproval.failed)
	require.Equal(t, float64(-1), metricsAfterApproval.active)
	require.Empty(t, metricsAfterApproval.dropped)
	require.Empty(t, metricsAfterApproval.signalsDrop)

	// The second, valid approval on another gated loop, same lane callback.
	otherBefore := approvedToolCallsOn(t, lane.client, otherLoopID)
	otherMsg := &loopSettlementMsg{data: approveAnswer(t, otherGate, otherLoopID)}
	lane.callbacks["agent.approval_response"](t.Context(), otherMsg)
	otherRecord := loopRecordOf(t, c, otherLoopID)
	otherAfter := approvedToolCallsOn(t, lane.client, otherLoopID)
	t.Logf("W4 second approval: %s; approved tool.execute %d->%d; record %s pending_approval=%v",
		dispositionOf(otherMsg), otherBefore, otherAfter, otherRecord.entity.State, otherRecord.entity.PendingApproval != nil)
	require.Zero(t, otherMsg.acks.Load()+otherMsg.naks.Load()+otherMsg.terms.Load(),
		"OBSERVED W4: the latched lane refuses a valid answer unsettled")
	require.Equal(t, otherBefore, otherAfter, "the valid answer dispatched nothing")
	require.Equal(t, agentic.LoopStateAwaitingApproval, otherRecord.entity.State)
	require.Contains(t, lane.logs.String(), `msg="Loop delivery refused by latched lane" lane=agent.approval_response`)
}
