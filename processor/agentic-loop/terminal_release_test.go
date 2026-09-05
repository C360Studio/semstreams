package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// terminalReaderProbe is a trajectory fact bucket that snapshots the loop's
// in-process entity at the instant each fact is created. Fact creation happens
// inside the terminal observation, which is the last reader the release must
// wait for, so "the loop was still present when the fact was created" is a
// happens-before the release cannot fake.
type terminalReaderProbe struct {
	*trajectoryTestBucket
	mu       sync.Mutex
	lm       *LoopManager
	loopID   string
	facts    []agentic.TrajectoryFactV1
	entities []agentic.LoopEntity
	found    []bool
}

func (p *terminalReaderProbe) Create(
	ctx context.Context, key string, value []byte, opts ...jetstream.KVCreateOpt,
) (uint64, error) {
	rev, err := p.trajectoryTestBucket.Create(ctx, key, value, opts...)
	if err != nil {
		return rev, err
	}
	var fact agentic.TrajectoryFactV1
	if uerr := json.Unmarshal(value, &fact); uerr != nil {
		return rev, uerr
	}
	entity, getErr := p.lm.GetLoop(p.loopID)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.facts = append(p.facts, fact)
	p.found = append(p.found, getErr == nil)
	p.entities = append(p.entities, entity)
	return rev, nil
}

// terminalObservation returns the loop's terminal fact and the loop entity as
// it stood when that fact was written — the last moment before the release.
func (p *terminalReaderProbe) terminalObservation() (agentic.TrajectoryFactV1, agentic.LoopEntity, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.facts) - 1; i >= 0; i-- {
		if p.facts[i].Kind == agentic.TrajectoryKindLoopTerminal {
			return p.facts[i], p.entities[i], p.found[i]
		}
	}
	return agentic.TrajectoryFactV1{}, agentic.LoopEntity{}, false
}

// terminalLoop is terminalObservation's entity, failing the test when no
// terminal observation was recorded at all.
func (p *terminalReaderProbe) terminalLoop(t *testing.T) agentic.LoopEntity {
	t.Helper()
	fact, entity, present := p.terminalObservation()
	if fact.Kind != agentic.TrajectoryKindLoopTerminal {
		t.Fatal("no terminal observation was recorded")
	}
	if !present {
		t.Fatal("the terminal observation ran AFTER the release: it saw no loop")
	}
	return entity
}

// newTerminalReaderProbe wires a probe onto the component so every trajectory
// fact it records snapshots loopID's in-process entity as it is written.
func newTerminalReaderProbe(c *Component, loopID string) *terminalReaderProbe {
	probe := &terminalReaderProbe{
		trajectoryTestBucket: &trajectoryTestBucket{values: make(map[string][]byte)},
		lm:                   c.handler.loopManager,
		loopID:               loopID,
	}
	// A no-op audit reporter: these tests observe RELEASE ORDERING, and the
	// probe has no store registry, so every evidence capture would otherwise
	// degrade Health for a reason unrelated to what is under test.
	c.trajectoryRecorder = newTrajectoryRecorder(probe, nil, "objectstore", func(trajectoryAuditFailure) {})
	return probe
}

// perLoopMapCount reports how many of the loop manager's per-loop maps still
// hold an entry for loopID. Reading the maps directly is the point: the
// requirement is about the maps, and a public accessor would only re-derive
// what the maps say.
func perLoopMapCount(m *LoopManager, loopID string) map[string]bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	held := map[string]bool{}
	if _, ok := m.loops[loopID]; ok {
		held["loops"] = true
	}
	if _, ok := m.contextManagers[loopID]; ok {
		held["contextManagers"] = true
	}
	if _, ok := m.pendingTools[loopID]; ok {
		held["pendingTools"] = true
	}
	if _, ok := m.queuedToolCalls[loopID]; ok {
		held["queuedToolCalls"] = true
	}
	if _, ok := m.cachedTools[loopID]; ok {
		held["cachedTools"] = true
	}
	if _, ok := m.cachedToolChoice[loopID]; ok {
		held["cachedToolChoice"] = true
	}
	if _, ok := m.cachedMetadata[loopID]; ok {
		held["cachedMetadata"] = true
	}
	if _, ok := m.cachedRequestTimeout[loopID]; ok {
		held["cachedRequestTimeout"] = true
	}
	if _, ok := m.cachedResponseFormat[loopID]; ok {
		held["cachedResponseFormat"] = true
	}
	if _, ok := m.taskPrompts[loopID]; ok {
		held["taskPrompts"] = true
	}
	if _, ok := m.truncationRetryAttempts[loopID]; ok {
		held["truncationRetryAttempts"] = true
	}
	for k, owner := range m.requestToLoop {
		if owner == loopID {
			held["requestToLoop:"+k] = true
		}
	}
	for k, owner := range m.toolCallToLoop {
		if owner == loopID {
			held["toolCallToLoop:"+k] = true
		}
	}
	return held
}

// populatedLoop drives a loop far enough that every per-loop map the manager
// holds has an entry for it, so "released" is a claim about all of them and
// not just the entity.
func populatedLoop(t *testing.T, h *MessageHandler) string {
	t.Helper()
	loopID := h.loopManager.GenerateLoopID()
	if _, err := h.loopManager.CreateLoopWithID(loopID, "task-populated", "general", "model-a", 5); err != nil {
		t.Fatalf("CreateLoopWithID: %v", err)
	}
	cm := h.loopManager.GetContextManager(loopID)
	if err := cm.AddMessage(RegionRecentHistory, agentic.ChatMessage{
		Role: "user", Content: "a conversation worth several kilobytes",
	}); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	if err := h.loopManager.AddPendingTool(loopID, "toolu_model_authored"); err != nil {
		t.Fatalf("AddPendingTool: %v", err)
	}
	h.loopManager.QueueToolCalls(loopID, []agentic.ToolCall{{ID: "queued-1", Name: "search"}})
	h.loopManager.CacheTools(loopID, []agentic.ToolDefinition{{Name: "search"}})
	h.loopManager.CacheToolChoice(loopID, &agentic.ToolChoice{Mode: "auto"})
	h.loopManager.CacheMetadata(loopID, map[string]any{"domain": "ops"})
	h.loopManager.CacheRequestTimeout(loopID, "30s")
	h.loopManager.CacheResponseFormat(loopID, &agentic.ResponseFormat{Type: "json_object"})
	h.loopManager.CacheTaskPrompt(loopID, "the original task prompt")
	h.loopManager.IncrementTruncationRetry(loopID)
	h.loopManager.TrackRequest(h.loopManager.GenerateRequestID(loopID), loopID)
	// A framework execution ID has no loop prefix, so only the routing owner
	// value sweep reaches it. Missing it would retain a route to a released loop.
	h.loopManager.TrackToolCall("execution-model-authored", loopID)
	h.loopManager.TrackToolName("toolu_model_authored", "search")
	return loopID
}

func releaseTestComponent(t *testing.T, h *MessageHandler) *Component {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h.logger = logger
	return &Component{
		config:    DefaultConfig(),
		handler:   h,
		logger:    logger,
		decoder:   message.NewDecoder(payloadbuiltins.NewTestRegistry(t)),
		started:   true,
		startTime: time.Now(),
	}
}

// TestTerminalReleaseClearsEveryPerLoopMap is the #1233 claim: a settled loop
// leaves nothing behind. Before this, DeleteLoop had no production caller, so a
// process retained every conversation it had ever run.
func TestTerminalReleaseClearsEveryPerLoopMap(t *testing.T) {
	h := NewMessageHandler(DefaultConfig())
	c := releaseTestComponent(t, h)
	loopID := populatedLoop(t, h)

	held := perLoopMapCount(h.loopManager, loopID)
	if len(held) < 13 {
		t.Fatalf("fixture populated only %d per-loop entries (%v); it must exercise every map", len(held), held)
	}

	c.releaseLoopTransientState(loopID)

	if held := perLoopMapCount(h.loopManager, loopID); len(held) != 0 {
		t.Fatalf("per-loop entries surviving release: %v", held)
	}
	if _, exists := h.loopManager.GetLoopForToolCall("execution-model-authored"); exists {
		t.Fatal("an execution ID still routes to the released loop; " +
			"lookup would resolve a loop that is gone and HandleToolResult would fail on it")
	}
	if _, exists := h.loopManager.GetLoopForToolCallWithRecovery("execution-model-authored"); exists {
		t.Fatal("lookup resolved a released loop")
	}
}

// TestTerminalReleaseIsIdempotent is I9. Four production terminal paths reach
// the release and two of them can both run for one loop; release must never be
// the thing that turns cleanup into a failure.
func TestTerminalReleaseIsIdempotent(t *testing.T) {
	h := NewMessageHandler(DefaultConfig())
	c := releaseTestComponent(t, h)
	loopID := populatedLoop(t, h)

	c.releaseLoopTransientState(loopID)
	after := perLoopMapCount(h.loopManager, loopID)
	for i := 0; i < 3; i++ {
		c.releaseLoopTransientState(loopID)
	}
	if again := perLoopMapCount(h.loopManager, loopID); len(again) != len(after) {
		t.Fatalf("release is not idempotent: %v then %v", after, again)
	}

	// Releasing a loop that never existed is also a no-op, not a fault.
	c.releaseLoopTransientState("never-registered")
	if held := perLoopMapCount(h.loopManager, "never-registered"); len(held) != 0 {
		t.Fatalf("releasing an unknown loop invented state: %v", held)
	}
}

// TestTerminalReleaseHappensAfterTerminalReaders is task 7.5. The release is
// deferred behind the terminal observation and the terminal event build; if it
// ran first, the observation would record an absent loop and the failure event
// — the input to the terminal graph write — could not be built at all.
func TestTerminalReleaseHappensAfterTerminalReaders(t *testing.T) {
	h := NewMessageHandler(DefaultConfig())
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	c := releaseTestComponent(t, h)
	// AFTER releaseTestComponent, which installs its own discarding logger.
	h.logger = logger
	c.logger = logger
	loopID := populatedLoop(t, h)
	probe := newTerminalReaderProbe(c, loopID)

	entity, err := h.loopManager.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop: %v", err)
	}
	c.handleLoopFailure(context.Background(), loopID, entity, "handler_error", errors.New("boom"))

	if state := probe.terminalLoop(t).State; state != agentic.LoopStateFailed {
		t.Fatalf("terminal observation saw state %q, want failed", state)
	}
	fact, _, _ := probe.terminalObservation()
	if fact.Status != agentic.TrajectoryStatusFailed {
		t.Fatalf("terminal fact status = %q, want failed", fact.Status)
	}

	// BuildFailureMessages is the other terminal reader: it reads the loop to
	// build the failure event that persistFailureState and the graph stamp
	// consume. If the release had preceded it, it would have logged this.
	if strings.Contains(logs.String(), "Failed to build failure event") {
		t.Fatalf("the failure-event build ran after the release:\n%s", logs.String())
	}

	if held := perLoopMapCount(h.loopManager, loopID); len(held) != 0 {
		t.Fatalf("terminal path did not release: %v", held)
	}
}

// TestLateApprovalResponseForSettledLoopIsExpectedDrop is I8 for the approval
// reader: a response for a released loop and a response for a still-present
// terminal loop must produce the same outcome. Before this, absence returned an
// error out of ResolveApprovalIfPending and the component logged it at ERROR,
// which is an expected steady state reported as a fault.
func TestLateApprovalResponseForSettledLoopIsExpectedDrop(t *testing.T) {
	ctx := context.Background()

	run := func(t *testing.T, release bool) (logs string, facts int) {
		t.Helper()
		h := NewMessageHandler(DefaultConfig())
		var captured strings.Builder
		logger := slog.New(slog.NewTextHandler(&captured, &slog.HandlerOptions{Level: slog.LevelWarn}))
		c := releaseTestComponent(t, h)
		// AFTER releaseTestComponent, which installs its own discarding logger.
		h.logger = logger
		c.logger = logger
		loopID := populatedLoop(t, h)
		if err := h.loopManager.TransitionLoop(loopID, agentic.LoopStateFailed); err != nil {
			t.Fatalf("TransitionLoop: %v", err)
		}
		if release {
			c.releaseLoopTransientState(loopID)
		}
		// Wired AFTER any release so the probe counts only what the late
		// arrival itself records. A stale drop must do NO handler-result work:
		// it neither resolved an approval nor advanced the loop, so it must not
		// re-enter the persistence-and-observation path. Comparing the two runs
		// is what makes "indistinguishable" a measurement rather than a claim —
		// the present-terminal case is the one that would record a terminal
		// fact if the drop fell through.
		probe := newTerminalReaderProbe(c, loopID)

		response := agentic.ApprovalResponse{
			LoopID: loopID, CallID: "toolu_model_authored",
			Decision: agentic.ApprovalDecisionApprove, ApprovedBy: "operator",
			DecidedAt: time.Now().UTC(),
		}
		envelope := message.NewBaseMessage(response.Schema(), &response, "test")
		data, err := json.Marshal(envelope)
		if err != nil {
			t.Fatalf("marshal approval response: %v", err)
		}
		c.handleApprovalResponseMessage(ctx, data)

		result, handlerErr := h.HandleApprovalResponse(ctx, response)
		if handlerErr != nil {
			t.Fatalf("late approval response returned an error (release=%v): %v", release, handlerErr)
		}
		if !result.staleDrop {
			t.Fatalf("late approval response was not a stale drop (release=%v)", release)
		}
		if len(result.PublishedMessages) != 0 {
			t.Fatalf("late approval response dispatched %d messages (release=%v)",
				len(result.PublishedMessages), release)
		}
		probe.mu.Lock()
		defer probe.mu.Unlock()
		return captured.String(), len(probe.facts)
	}

	presentLogs, presentFacts := run(t, false)
	absentLogs, absentFacts := run(t, true)

	if strings.Contains(presentLogs, "ERROR") || strings.Contains(absentLogs, "ERROR") {
		t.Fatalf("a late approval response was reported as a failure:\n present: %s\n absent: %s",
			presentLogs, absentLogs)
	}
	const dropLine = "approval response ignored: not awaiting or call_id mismatch"
	if !strings.Contains(presentLogs, dropLine) || !strings.Contains(absentLogs, dropLine) {
		t.Fatalf("the two cases do not produce the same declared drop:\n present: %s\n absent: %s",
			presentLogs, absentLogs)
	}
	if presentFacts != 0 || absentFacts != 0 || presentFacts != absentFacts {
		t.Fatalf("a stale drop re-entered the persistence-and-observation path: "+
			"present recorded %d facts, absent recorded %d — the two are not indistinguishable",
			presentFacts, absentFacts)
	}
}

// TestLateToolResultForSettledLoopIsExpectedDrop is I8 for the tool-result and
// model-response readers. Both resolve a loop from a routing map the release
// clears; the drop is counted and warned, never an error.
func TestLateToolResultForSettledLoopIsExpectedDrop(t *testing.T) {
	ctx := context.Background()
	h := NewMessageHandler(DefaultConfig())
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	c := releaseTestComponent(t, h)
	// AFTER releaseTestComponent, which installs its own discarding logger.
	h.logger = logger
	c.logger = logger
	c.metrics = nil
	loopID := populatedLoop(t, h)
	requestID := h.loopManager.GenerateRequestID(loopID)
	h.loopManager.TrackRequest(requestID, loopID)

	if err := h.loopManager.TransitionLoop(loopID, agentic.LoopStateComplete); err != nil {
		t.Fatalf("TransitionLoop: %v", err)
	}
	c.releaseLoopTransientState(loopID)

	toolResult := agentic.ToolResult{
		ExecutionID: "execution-model-authored", CallID: "toolu_model_authored", Name: "search", Content: "late",
	}
	toolEnvelope := message.NewBaseMessage(toolResult.Schema(), &toolResult, "test")
	toolData, err := json.Marshal(toolEnvelope)
	if err != nil {
		t.Fatalf("marshal tool result: %v", err)
	}
	c.handleToolResultMessage(ctx, toolData)

	response := agentic.AgentResponse{
		RequestID: requestID, Status: agentic.StatusComplete,
		Message: agentic.ChatMessage{Role: "assistant", Content: "late answer"},
	}
	respEnvelope := message.NewBaseMessage(response.Schema(), &response, "test")
	respData, err := json.Marshal(respEnvelope)
	if err != nil {
		t.Fatalf("marshal agent response: %v", err)
	}
	c.handleResponseMessage(ctx, respData)

	out := logs.String()
	if strings.Contains(out, "ERROR") {
		t.Fatalf("a late arrival for a settled loop was reported as a failure:\n%s", out)
	}
	if !strings.Contains(out, "No loop found for tool execution") {
		t.Fatalf("late tool result was not declared as a drop:\n%s", out)
	}
	if !strings.Contains(out, "No loop found for request") {
		t.Fatalf("late model response was not declared as a drop:\n%s", out)
	}
	// The loop must stay gone: a late arrival never resurrects per-loop state.
	if held := perLoopMapCount(h.loopManager, loopID); len(held) != 0 {
		t.Fatalf("a late arrival re-registered per-loop state: %v", held)
	}
}

// TestLateModelResponseForSettledLoopIsExpectedDrop is I8 for the model-response
// reader on its own, and it is the reader's own test rather than a line inside
// the tool-result one: §7 made this drop common, and the claim being made about
// it is that the drop is DECLARED — a warn AND a counter, the same pair its
// sibling one function away already emits — not merely quiet.
//
// spec: agentic-loop / Requirement: Per-loop in-process state is released at terminal, through the one release point
func TestLateModelResponseForSettledLoopIsExpectedDrop(t *testing.T) {
	ctx := context.Background()
	h := NewMessageHandler(DefaultConfig())
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	c := releaseTestComponent(t, h)
	// AFTER releaseTestComponent, which installs its own discarding logger.
	h.logger = logger
	c.logger = logger
	c.metrics = getMetrics(nil)
	before := testutil.ToFloat64(c.metrics.modelResponsesDropped.WithLabelValues("stale_request_id"))

	loopID := populatedLoop(t, h)
	requestID := h.loopManager.GenerateRequestID(loopID)
	h.loopManager.TrackRequest(requestID, loopID)

	if err := h.loopManager.TransitionLoop(loopID, agentic.LoopStateComplete); err != nil {
		t.Fatalf("TransitionLoop: %v", err)
	}
	c.releaseLoopTransientState(loopID)

	response := agentic.AgentResponse{
		RequestID: requestID, Status: agentic.StatusComplete,
		Message: agentic.ChatMessage{Role: "assistant", Content: "late answer"},
	}
	respEnvelope := message.NewBaseMessage(response.Schema(), &response, "test")
	respData, err := json.Marshal(respEnvelope)
	if err != nil {
		t.Fatalf("marshal agent response: %v", err)
	}
	c.handleResponseMessage(ctx, respData)

	out := logs.String()
	if strings.Contains(out, "ERROR") {
		t.Fatalf("a late model response for a settled loop was reported as a failure:\n%s", out)
	}
	if !strings.Contains(out, "No loop found for request") {
		t.Fatalf("late model response was not declared as a drop:\n%s", out)
	}
	after := testutil.ToFloat64(c.metrics.modelResponsesDropped.WithLabelValues("stale_request_id"))
	if d := after - before; d != 1 {
		t.Fatalf("model_responses_dropped_total{reason=stale_request_id} delta = %v, want 1 — "+
			"the drop is logged but not counted, so an operator cannot see it", d)
	}
	if held := perLoopMapCount(h.loopManager, loopID); len(held) != 0 {
		t.Fatalf("a late model response re-registered per-loop state: %v", held)
	}
}

// TestApprovalSweepUnaffectedByTerminalRelease: a sweep candidate is by
// definition not terminal, so released loops can only ever contribute nothing.
func TestApprovalSweepUnaffectedByTerminalRelease(t *testing.T) {
	h := NewMessageHandler(DefaultConfig())
	c := releaseTestComponent(t, h)

	awaiting := setUpAwaitingLoop(t, h, time.Minute, 90*time.Second)

	settled := populatedLoop(t, h)
	if err := h.loopManager.TransitionLoop(settled, agentic.LoopStateComplete); err != nil {
		t.Fatalf("TransitionLoop: %v", err)
	}
	c.releaseLoopTransientState(settled)

	candidates := h.loopManager.SnapshotExpiredApprovals(time.Now())
	if len(candidates) != 1 || candidates[0].LoopID != awaiting {
		t.Fatalf("expired-approval candidates = %+v, want exactly the awaiting loop", candidates)
	}
}

// settledLoopsKV is the durable AGENT_LOOPS read the loop-result tool consumes.
// Keys use the production COMPLETE_{loopID} shape.
type settledLoopsKV struct {
	data map[string][]byte
}

func (k *settledLoopsKV) Get(_ context.Context, key string) (*natsclient.KVEntry, error) {
	value, ok := k.data[key]
	if !ok {
		return nil, natsclient.ErrKVKeyNotFound
	}
	return &natsclient.KVEntry{Key: key, Value: value, Revision: 1}, nil
}

// TestSettledLoopResultReadableAfterRelease locks the half of the release
// invariant that says a settled loop's ANSWER survives: the durable loop record
// is the authority, and reading it must not depend on the in-process maps.
//
// The assertion is deliberately over the real production executor rather than
// over the KV bytes: if the loop-result path ever started consulting the loop
// manager as a "fast path", this test is what breaks. Today it cannot — the
// executor's whole dependency is a LoopsKVReader — and that is the property
// being pinned, not merely re-observed.
func TestSettledLoopResultReadableAfterRelease(t *testing.T) {
	ctx := context.Background()
	h := NewMessageHandler(DefaultConfig())
	c := releaseTestComponent(t, h)
	loopID := populatedLoop(t, h)

	const answer = "the settled loop's full answer, retained durably"
	completion := agentic.LoopCompletedEvent{
		LoopID: loopID, TaskID: "task-populated", Outcome: agentic.OutcomeSuccess,
		Result: answer, Role: "general", CompletedAt: time.Now().UTC(),
	}
	encoded, err := json.Marshal(&completion)
	if err != nil {
		t.Fatalf("marshal completion: %v", err)
	}
	kv := &settledLoopsKV{data: map[string][]byte{"COMPLETE_" + loopID: encoded}}

	if err := h.loopManager.TransitionLoop(loopID, agentic.LoopStateComplete); err != nil {
		t.Fatalf("TransitionLoop: %v", err)
	}
	c.releaseLoopTransientState(loopID)
	if held := perLoopMapCount(h.loopManager, loopID); len(held) != 0 {
		t.Fatalf("fixture did not release: %v", held)
	}

	result, err := agentictools.NewReadLoopResultExecutor(kv).Execute(ctx, agentic.ToolCall{
		ID:        "call-read",
		Name:      agentictools.ReadLoopResultToolName,
		Arguments: map[string]any{"loop_id": loopID},
	})
	if err != nil {
		t.Fatalf("read_loop_result on a released loop: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("read_loop_result error = %q, want the result", result.Error)
	}
	if !strings.Contains(result.Content, answer) {
		t.Fatalf("read_loop_result content = %q, want the settled answer", result.Content)
	}
}
