package agenticloop

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
)

// setUpAwaitingLoop creates a loop, drives it into LoopStateAwaitingApproval
// with a known timeout, and returns the loop ID. Helper for the
// timeout-sweep tests below.
func setUpAwaitingLoop(t *testing.T, handler *MessageHandler, timeout time.Duration, requestedAtOffset time.Duration) string {
	t.Helper()
	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}

	entity, err := handler.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop: %v", err)
	}
	if err := entity.BeginAwaitingApproval(
		"call-gated",
		"delete_rule",
		map[string]any{"rule_id": "rule-42"},
		"approval_required: ...",
		timeout,
		"trace-1",
	); err != nil {
		t.Fatalf("BeginAwaitingApproval: %v", err)
	}
	// Backdate RequestedAt to simulate elapsed time without sleeping.
	entity.PendingApproval.RequestedAt = time.Now().UTC().Add(-requestedAtOffset)
	if err := handler.UpdateLoop(entity); err != nil {
		t.Fatalf("UpdateLoop: %v", err)
	}
	return loopID
}

func TestSnapshotExpiredApprovals_PicksUpExpiredOnly(t *testing.T) {
	t.Parallel()

	handler := NewMessageHandler(DefaultConfig())

	// Three loops: expired, fresh, indefinite (timeout=0).
	expiredID := setUpAwaitingLoop(t, handler, 1*time.Minute, 90*time.Second) // expired
	freshID := setUpAwaitingLoop(t, handler, 1*time.Minute, 10*time.Second)   // not yet
	indefiniteID := setUpAwaitingLoop(t, handler, 0, 24*time.Hour)            // never expires

	candidates := handler.loopManager.SnapshotExpiredApprovals(time.Now().UTC())

	gotIDs := map[string]bool{}
	for _, c := range candidates {
		gotIDs[c.LoopID] = true
	}
	if !gotIDs[expiredID] {
		t.Errorf("expected expired loop %s in snapshot; got %v", expiredID, gotIDs)
	}
	if gotIDs[freshID] {
		t.Errorf("fresh loop %s should not be in snapshot", freshID)
	}
	if gotIDs[indefiniteID] {
		t.Errorf("timeout=0 loop %s should not be in snapshot (wait-indefinitely)", indefiniteID)
	}
}

func TestSnapshotExpiredApprovals_SkipsNonAwaitingLoops(t *testing.T) {
	t.Parallel()

	handler := NewMessageHandler(DefaultConfig())

	// A loop that's not in awaiting_approval state must be skipped
	// even if PendingApproval is set (defensive).
	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}
	// Don't transition to awaiting; leave default state.

	candidates := handler.loopManager.SnapshotExpiredApprovals(time.Now().UTC())
	for _, c := range candidates {
		if c.LoopID == loopID {
			t.Errorf("non-awaiting loop %s should not appear in snapshot", loopID)
		}
	}
}

func TestSnapshotExpiredApprovals_CarriesCallIDAndToolName(t *testing.T) {
	t.Parallel()

	handler := NewMessageHandler(DefaultConfig())
	expiredID := setUpAwaitingLoop(t, handler, 1*time.Minute, 90*time.Second)

	candidates := handler.loopManager.SnapshotExpiredApprovals(time.Now().UTC())
	var got *ApprovalTimeoutCandidate
	for i := range candidates {
		if candidates[i].LoopID == expiredID {
			got = &candidates[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("expected expired loop %s in snapshot", expiredID)
	}
	if got.CallID != "call-gated" {
		t.Errorf("CallID = %q, want %q", got.CallID, "call-gated")
	}
	if got.ToolName != "delete_rule" {
		t.Errorf("ToolName = %q, want %q", got.ToolName, "delete_rule")
	}
	if got.Timeout != 1*time.Minute {
		t.Errorf("Timeout = %v, want 1m", got.Timeout)
	}
}

// TestHandleApprovalResponse_PanicRecovered confirms that a panic in
// the approval handler doesn't propagate out — the caller gets a
// benign empty result and the loop stays in awaiting_approval so the
// sweeper picks it up later. Triggers the panic by passing an
// internally inconsistent state (response with empty CallID after
// validation passes — caught by post-validate paths).
//
// The test exercises the defer-recover via the resolve path: forcing
// loopManager into a state where the resolve helper will nil-deref or
// panic. We don't have an easy panic injection — Validate() catches
// most invalid shapes — so we instead verify the recover path's shape
// by direct invocation through a mock-friendly test that triggers a
// nil-deref via a synthetic loopID lookup race.
//
// Concretely: HandleApprovalResponse looks up the loop via
// ResolveApprovalIfPending, then GetLoop. If GetLoop fails (loop
// vanished mid-call — practically a tested race), we fall through
// without a panic. To genuinely force a panic, we'd need to inject a
// faulty handler — out of scope for this unit test. The
// approval_sweeper_panic_integration test in C4 covers a real
// faulty-handler path using a wrapped logger.
//
// What this test DOES verify: HandleApprovalResponse with a
// validated-but-stale response (loop already resolved or never
// awaiting) does NOT panic and returns a benign empty result. This is
// the most common production "weird state" the recovery would need
// to swallow.
func TestHandleApprovalResponse_StaleResponseReturnsBenignEmpty(t *testing.T) {
	t.Parallel()

	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-1", "general", "m", 20)
	if err != nil {
		t.Fatalf("CreateLoop: %v", err)
	}

	// Loop is NOT in awaiting_approval; response will be a stale-drop.
	response := agentic.ApprovalResponse{
		LoopID:    loopID,
		CallID:    "call-1",
		Decision:  agentic.ApprovalDecisionReject,
		Reason:    "test",
		DecidedAt: time.Now().UTC(),
	}

	result, err := handler.HandleApprovalResponse(context.Background(), response)
	if err != nil {
		t.Fatalf("HandleApprovalResponse: %v (should be a benign drop)", err)
	}
	if result.LoopID != loopID {
		t.Errorf("result.LoopID = %q, want %q", result.LoopID, loopID)
	}
}

// TestApprovalTimeoutAutoReject_DrivesHandleApprovalResponse confirms
// the sweeper's per-candidate logic: a loop awaiting approval with an
// elapsed timeout, fed an auto-rejection ApprovalResponse, transitions
// out of awaiting and lands a synth-rejection in the result map with
// the timeout diagnostic. This is what the sweeper does for each
// candidate; the goroutine + ticker plumbing is the only piece the
// component-level lifecycle test would add.
func TestApprovalTimeoutAutoReject_DrivesHandleApprovalResponse(t *testing.T) {
	t.Parallel()

	handler := NewMessageHandler(DefaultConfig())
	loopID := setUpAwaitingLoop(t, handler, 1*time.Minute, 90*time.Second)

	// Mirror the payload the sweeper builds for an expired candidate.
	response := agentic.ApprovalResponse{
		LoopID:     loopID,
		CallID:     "call-gated",
		Decision:   agentic.ApprovalDecisionReject,
		Reason:     "approval timed out after 1m",
		ApprovedBy: approvalTimeoutSystemApprover,
		DecidedAt:  time.Now().UTC(),
	}

	result, err := handler.HandleApprovalResponse(context.Background(), response)
	if err != nil {
		t.Fatalf("HandleApprovalResponse: %v", err)
	}

	entity, err := handler.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop: %v", err)
	}
	if entity.State == agentic.LoopStateAwaitingApproval {
		t.Errorf("loop still awaiting_approval after auto-reject; state = %s", entity.State)
	}
	if entity.PendingApproval != nil {
		t.Errorf("PendingApproval should be cleared; got %+v", entity.PendingApproval)
	}

	// The trajectory step records the rejection — assert on that
	// rather than PendingToolResults because the synth flows through
	// HandleToolResult → handleToolsComplete which drains the result
	// map into context messages before this test can observe it.
	if len(result.TrajectorySteps) == 0 {
		t.Fatal("expected trajectory step for rejection")
	}
	step := result.TrajectorySteps[0]
	if !strings.Contains(step.ErrorMessage, agentic.ApprovalRejectedPrefix) {
		t.Errorf("trajectory step ErrorMessage %q missing ApprovalRejectedPrefix", step.ErrorMessage)
	}
	if !strings.Contains(step.ErrorMessage, "timed out") {
		t.Errorf("trajectory step ErrorMessage %q should mention timeout diagnostic", step.ErrorMessage)
	}
	// Sweeper-driven auto-rejects use the distinct
	// approvalTimeoutSystemApprover sentinel so observers can
	// distinguish framework auto-rejects from human anonymous
	// rejects. The diagnostic carries the sentinel verbatim.
	if !strings.Contains(step.ErrorMessage, approvalTimeoutSystemApprover) {
		t.Errorf("trajectory step ErrorMessage %q should carry %q sentinel",
			step.ErrorMessage, approvalTimeoutSystemApprover)
	}
}

// TestSnapshotExpiredApprovals_StableUnderRepeatedSweeps checks the
// idempotent contract: a fresh loop stays a candidate-zero across
// multiple snapshot calls (no spurious-expiration drift). Cheap test
// for the hot path of the sweeper.
func TestSnapshotExpiredApprovals_StableUnderRepeatedSweeps(t *testing.T) {
	t.Parallel()

	handler := NewMessageHandler(DefaultConfig())
	setUpAwaitingLoop(t, handler, 1*time.Minute, 10*time.Second) // not expired

	for range 5 {
		if got := handler.loopManager.SnapshotExpiredApprovals(time.Now().UTC()); len(got) != 0 {
			t.Errorf("expected zero candidates; got %d", len(got))
		}
	}
}

// TestRunApprovalTimeoutSweeper_ExitsOnContextCancel confirms the
// sweeper goroutine respects parent ctx cancellation. Catches future
// regressions where someone replaces the select with a time.Sleep
// loop that ignores ctx.
func TestRunApprovalTimeoutSweeper_ExitsOnContextCancel(t *testing.T) {
	t.Parallel()

	c := &Component{
		handler: NewMessageHandler(DefaultConfig()),
		logger:  slog.Default(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.runApprovalTimeoutSweeper(ctx)
	}()

	// Give the goroutine a brief moment to enter the select.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sweeper did not exit within 2s after ctx cancel")
	}
}

// TestSweepExpiredApprovals_PublishesApprovalResponseToWire verifies that
// sweepExpiredApprovals publishes the synthesised ApprovalResponse to the
// agent.approval_response.<loopID> subject so wire observers (dashboards,
// audit consumers, sister repos) see timeout auto-rejects symmetrically
// with human approvals.
//
// Uses the testPublishHook seam (set via SetTestPublishHook / export_test.go)
// to capture wire-level publishes without a real NATS connection. Explicit
// channel synchronisation — no time.Sleep.
func TestSweepExpiredApprovals_PublishesApprovalResponseToWire(t *testing.T) {
	t.Parallel()

	// Build a Component with an expired awaiting-approval loop.
	cfg := DefaultConfig()
	c := &Component{
		config:  cfg,
		handler: NewMessageHandler(cfg),
		logger:  slog.Default(),
	}

	loopID := setUpAwaitingLoop(t, c.handler, 1*time.Minute, 90*time.Second)

	// Wire the testPublishHook to capture all wire-level publishes.
	type wireMsg struct {
		subject string
		data    []byte
	}
	published := make(chan wireMsg, 8) // buffered: sweep is synchronous
	c.SetTestPublishHook(func(subject string, data []byte) {
		published <- wireMsg{subject: subject, data: data}
	})

	c.sweepExpiredApprovals(context.Background())

	// The hook must have received at least one message whose subject
	// matches agent.approval_response.<loopID>.
	expectedSubject := "agent.approval_response." + loopID
	select {
	case msg := <-published:
		if msg.subject != expectedSubject {
			t.Errorf("wire publish subject = %q, want %q", msg.subject, expectedSubject)
		}
		// Verify it is a valid BaseMessage envelope carrying an ApprovalResponse.
		var envelope struct {
			Type struct {
				Domain   string `json:"domain"`
				Category string `json:"category"`
			} `json:"type"`
			Payload struct {
				LoopID   string `json:"loop_id"`
				Decision string `json:"decision"`
				Reason   string `json:"reason"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(msg.data, &envelope); err != nil {
			t.Fatalf("decode wire envelope: %v", err)
		}
		if envelope.Type.Category != agentic.CategoryApprovalResponse {
			t.Errorf("envelope type.category = %q, want %q",
				envelope.Type.Category, agentic.CategoryApprovalResponse)
		}
		if envelope.Payload.LoopID != loopID {
			t.Errorf("envelope payload.loop_id = %q, want %q", envelope.Payload.LoopID, loopID)
		}
		if envelope.Payload.Decision != agentic.ApprovalDecisionReject {
			t.Errorf("envelope payload.decision = %q, want %q",
				envelope.Payload.Decision, agentic.ApprovalDecisionReject)
		}
		if !strings.Contains(envelope.Payload.Reason, "timed out") {
			t.Errorf("envelope payload.reason = %q, want it to mention 'timed out'", envelope.Payload.Reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sweepExpiredApprovals did not publish to agent.approval_response wire within 2s")
	}
}

// TestPublishApprovalResponseToWire_NilNATSClientIsNoop verifies that
// publishApprovalResponseToWire does not panic when natsClient is nil
// (pure unit tests, Stop race). Guards against a nil-deref regression.
func TestPublishApprovalResponseToWire_NilNATSClientIsNoop(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	c := &Component{
		config:  cfg,
		handler: NewMessageHandler(cfg),
		logger:  slog.Default(),
		// natsClient intentionally nil
	}

	response := agentic.ApprovalResponse{
		LoopID:     "loop-noop",
		CallID:     "call-noop",
		Decision:   agentic.ApprovalDecisionReject,
		Reason:     "approval timed out after 1m",
		ApprovedBy: approvalTimeoutSystemApprover,
		DecidedAt:  time.Now().UTC(),
	}

	// Must not panic.
	c.publishApprovalResponseToWire(context.Background(), response)
}
