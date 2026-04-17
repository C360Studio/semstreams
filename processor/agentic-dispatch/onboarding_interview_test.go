package agenticdispatch

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/agentic"

	operatingmodel "github.com/c360studio/semstreams/agentic/operating-model"
)

// captureSink collects every UserResponse the component tries to send. The
// interview handler uses c.sendResponse (not return values), so we need an
// in-process way to observe outputs.
type captureSink struct {
	mu        sync.Mutex
	responses []agentic.UserResponse
}

func (s *captureSink) add(r agentic.UserResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responses = append(s.responses, r)
}

func (s *captureSink) all() []agentic.UserResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]agentic.UserResponse, len(s.responses))
	copy(out, s.responses)
	return out
}

// newInterviewTestComponent wires a Component for interview-state-machine
// testing: logger and tracker, no NATS (publishLayerApproved skips silently
// when natsClient is nil). It also installs a response sink on the component
// by swapping sendResponse; callers read from the returned captureSink.
func newInterviewTestComponent(t *testing.T) (*Component, *captureSink) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sink := &captureSink{}
	c := &Component{
		logger:      logger,
		loopTracker: NewLoopTrackerWithLogger(logger),
	}
	c.sendResponseFn = sink.add
	return c, sink
}

// startOnboardingLoop seeds the tracker with an onboarding loop on a given
// layer and sub-state, mirroring the shape /onboard produces.
func startOnboardingLoop(c *Component, userID, channelID, layer string) string {
	resp, _ := c.handleOnboardCommand(context.Background(), agentic.UserMessage{
		UserID:      userID,
		ChannelType: "http",
		ChannelID:   channelID,
	}, nil, "")
	// Reshape onto the requested layer if different from default.
	if layer != operatingmodel.LayerOperatingRhythms {
		c.loopTracker.mu.Lock()
		if info, ok := c.loopTracker.loops[resp.InReplyTo]; ok {
			info.WorkflowStep = layer
		}
		c.loopTracker.mu.Unlock()
	}
	return resp.InReplyTo
}

func interviewUserMsg(userID, channelID, content string) agentic.UserMessage {
	return agentic.UserMessage{
		UserID:      userID,
		ChannelType: "http",
		ChannelID:   channelID,
		Content:     content,
	}
}

// -- isOnboardingInFlight --

func TestIsOnboardingInFlight_TrueForActiveOnboardingLoop(t *testing.T) {
	c, _ := newInterviewTestComponent(t)
	startOnboardingLoop(c, "coby", "s1", operatingmodel.LayerOperatingRhythms)

	if !c.isOnboardingInFlight("coby", "s1") {
		t.Errorf("isOnboardingInFlight = false, want true")
	}
}

func TestIsOnboardingInFlight_FalseForNonOnboardingLoop(t *testing.T) {
	c, _ := newInterviewTestComponent(t)
	c.loopTracker.Track(&LoopInfo{
		LoopID: "loop_xx", UserID: "coby", ChannelID: "s1",
		State: "pending",
	})
	if c.isOnboardingInFlight("coby", "s1") {
		t.Errorf("isOnboardingInFlight true for non-onboarding loop")
	}
}

func TestIsOnboardingInFlight_FalseForTerminalOnboardingLoop(t *testing.T) {
	c, _ := newInterviewTestComponent(t)
	loopID := startOnboardingLoop(c, "coby", "s1", operatingmodel.LayerOperatingRhythms)
	c.loopTracker.UpdateState(loopID, "complete")
	if c.isOnboardingInFlight("coby", "s1") {
		t.Errorf("isOnboardingInFlight true after loop marked complete")
	}
}

// -- answer → checkpoint transition --

func TestHandleOnboardingTurn_AnswerTransitionsToAwaitingApproval(t *testing.T) {
	c, sink := newInterviewTestComponent(t)
	loopID := startOnboardingLoop(c, "coby", "s1", operatingmodel.LayerOperatingRhythms)

	c.handleOnboardingTurn(context.Background(),
		interviewUserMsg("coby", "s1", "Mondays 9-10am planning block"))

	info := c.loopTracker.Get(loopID)
	if got := onboardSubState(info); got != SubStateAwaitingApproval {
		t.Errorf("sub-state after answer = %q, want %q", got, SubStateAwaitingApproval)
	}
	entries, ok := draftEntriesFromMetadata(info.Metadata)
	if !ok || len(entries) != 1 {
		t.Fatalf("draft entries after answer = %+v, want 1 entry", entries)
	}
	if entries[0].Summary != "Mondays 9-10am planning block" {
		t.Errorf("draft summary = %q, want full user text", entries[0].Summary)
	}
	if len(sink.all()) != 1 {
		t.Errorf("expected 1 response, got %d", len(sink.all()))
	}
}

func TestHandleOnboardingTurn_EmptyAnswerStaysInAwaitingAnswer(t *testing.T) {
	// Whitespace-only answer must not create a draft or advance sub-state;
	// the user is re-prompted while remaining in awaiting_answer.
	c, sink := newInterviewTestComponent(t)
	loopID := startOnboardingLoop(c, "coby", "s1", operatingmodel.LayerOperatingRhythms)

	c.handleOnboardingTurn(context.Background(),
		interviewUserMsg("coby", "s1", "   "))

	info := c.loopTracker.Get(loopID)
	if got := onboardSubState(info); got != SubStateAwaitingAnswer {
		t.Errorf("sub-state = %q, want awaiting_answer", got)
	}
	if entries, _ := draftEntriesFromMetadata(info.Metadata); len(entries) != 0 {
		t.Errorf("empty answer produced %d entries, want 0", len(entries))
	}
	if n := len(sink.all()); n != 1 {
		t.Errorf("expected 1 re-prompt, got %d", n)
	}
}

// -- approval advances layer --

func TestHandleOnboardingTurn_ApprovalAdvancesToNextLayer(t *testing.T) {
	c, sink := newInterviewTestComponent(t)
	loopID := startOnboardingLoop(c, "coby", "s1", operatingmodel.LayerOperatingRhythms)

	// Answer layer 1, then approve.
	c.handleOnboardingTurn(context.Background(),
		interviewUserMsg("coby", "s1", "Mondays 9-10am"))
	c.handleOnboardingTurn(context.Background(),
		interviewUserMsg("coby", "s1", "approve"))

	info := c.loopTracker.Get(loopID)
	if info.WorkflowStep != operatingmodel.LayerRecurringDecisions {
		t.Errorf("WorkflowStep after approval = %q, want %q",
			info.WorkflowStep, operatingmodel.LayerRecurringDecisions)
	}
	if got := onboardSubState(info); got != SubStateAwaitingAnswer {
		t.Errorf("sub-state after approval = %q, want %q", got, SubStateAwaitingAnswer)
	}
	// Draft should be cleared.
	if entries, _ := draftEntriesFromMetadata(info.Metadata); len(entries) != 0 {
		t.Errorf("draft entries should be cleared after approval, got %d", len(entries))
	}
	// Last response should be the next layer's opener.
	rs := sink.all()
	if n := len(rs); n != 2 {
		t.Fatalf("expected 2 responses (checkpoint + next opener), got %d", n)
	}
	if rs[1].Content != OnboardingOpeningQuestion(operatingmodel.LayerRecurringDecisions) {
		t.Errorf("post-approval response was not the next layer's opener:\n%s", rs[1].Content)
	}
}

func TestHandleOnboardingTurn_NonApprovalReplacesDraft(t *testing.T) {
	c, _ := newInterviewTestComponent(t)
	loopID := startOnboardingLoop(c, "coby", "s1", operatingmodel.LayerOperatingRhythms)

	c.handleOnboardingTurn(context.Background(),
		interviewUserMsg("coby", "s1", "first draft"))
	c.handleOnboardingTurn(context.Background(),
		interviewUserMsg("coby", "s1", "let me try again"))

	info := c.loopTracker.Get(loopID)
	if info.WorkflowStep != operatingmodel.LayerOperatingRhythms {
		t.Errorf("WorkflowStep advanced on non-approval: %q", info.WorkflowStep)
	}
	if got := onboardSubState(info); got != SubStateAwaitingApproval {
		t.Errorf("sub-state = %q, want awaiting_approval (draft replaced)", got)
	}
	entries, _ := draftEntriesFromMetadata(info.Metadata)
	if len(entries) != 1 || entries[0].Summary != "let me try again" {
		t.Errorf("draft after replacement = %+v, want the newer text", entries)
	}
}

// -- completion --

func TestHandleOnboardingTurn_FinalLayerApprovalCompletes(t *testing.T) {
	c, sink := newInterviewTestComponent(t)
	loopID := startOnboardingLoop(c, "coby", "s1", operatingmodel.LayerFriction)

	c.handleOnboardingTurn(context.Background(),
		interviewUserMsg("coby", "s1", "email blocks friction"))
	c.handleOnboardingTurn(context.Background(),
		interviewUserMsg("coby", "s1", "approve"))

	info := c.loopTracker.Get(loopID)
	if info.State != "complete" {
		t.Errorf("loop state after final approval = %q, want complete", info.State)
	}
	if info.Outcome != "success" {
		t.Errorf("loop outcome = %q, want success", info.Outcome)
	}
	rs := sink.all()
	final := rs[len(rs)-1]
	if final.Type != agentic.ResponseTypeStatus {
		t.Errorf("final response type = %q, want Status", final.Type)
	}
	if c.isOnboardingInFlight("coby", "s1") {
		t.Errorf("onboarding still in-flight after completion")
	}
}

func TestHandleOnboardingTurn_IterationsCountLayersSaved(t *testing.T) {
	// Iterations advances exactly once per approved layer so /status
	// accurately reports progress on partial runs.
	c, _ := newInterviewTestComponent(t)
	loopID := startOnboardingLoop(c, "coby", "s1", operatingmodel.LayerOperatingRhythms)

	// Answer + approve layer 1.
	c.handleOnboardingTurn(context.Background(),
		interviewUserMsg("coby", "s1", "weekly planning"))
	c.handleOnboardingTurn(context.Background(),
		interviewUserMsg("coby", "s1", "approve"))

	if got := c.loopTracker.Get(loopID).Iterations; got != 1 {
		t.Errorf("iterations after one approval = %d, want 1", got)
	}

	// Replacing the draft mid-layer 2 must NOT bump iterations.
	c.handleOnboardingTurn(context.Background(),
		interviewUserMsg("coby", "s1", "first draft"))
	c.handleOnboardingTurn(context.Background(),
		interviewUserMsg("coby", "s1", "better draft"))
	if got := c.loopTracker.Get(loopID).Iterations; got != 1 {
		t.Errorf("iterations after draft replacement = %d, want 1", got)
	}
}

func TestAdvanceToNextLayer_GuardedAgainstStaleSnapshot(t *testing.T) {
	// S2 guard: if the stored WorkflowStep no longer matches expectedLayer
	// (e.g. a concurrent mutation already moved on), the advance is a no-op.
	c, _ := newInterviewTestComponent(t)
	loopID := startOnboardingLoop(c, "coby", "s1", operatingmodel.LayerOperatingRhythms)

	// Sneak past to layer 3 directly — simulates a concurrent advance.
	c.loopTracker.mu.Lock()
	c.loopTracker.loops[loopID].WorkflowStep = operatingmodel.LayerDependencies
	c.loopTracker.mu.Unlock()

	// Now a caller holding a stale snapshot that thinks the current layer is
	// LayerOperatingRhythms tries to advance to LayerRecurringDecisions. The
	// guard must reject so we don't clobber the real position.
	applied := advanceToNextLayer(c.loopTracker, loopID,
		operatingmodel.LayerOperatingRhythms,
		operatingmodel.LayerRecurringDecisions, 2)
	if applied {
		t.Fatalf("advanceToNextLayer applied despite stale expected layer")
	}
	if got := c.loopTracker.Get(loopID).WorkflowStep; got != operatingmodel.LayerDependencies {
		t.Errorf("WorkflowStep = %q, want %q (preserved)",
			got, operatingmodel.LayerDependencies)
	}
}

func TestSnapshotLoop_IndependentFromWrites(t *testing.T) {
	// B1 guard: snapshot's Metadata must not share its backing map with the
	// tracker — mutating the tracker after the snapshot is taken must not
	// be visible in the snapshot.
	c, _ := newInterviewTestComponent(t)
	loopID := startOnboardingLoop(c, "coby", "s1", operatingmodel.LayerOperatingRhythms)

	snap := snapshotLoop(c.loopTracker, loopID)
	if snap == nil {
		t.Fatal("snapshot returned nil for active loop")
	}
	initialLayer := snap.WorkflowStep

	updateMetadata(c.loopTracker, loopID, map[string]any{
		OnboardMetaSubState: SubStateAwaitingApproval,
	})

	if got := onboardSubState(snap); got == SubStateAwaitingApproval {
		t.Errorf("snapshot observed post-snapshot write; got sub-state %q", got)
	}
	if snap.WorkflowStep != initialLayer {
		t.Errorf("snapshot layer mutated: got %q, want %q", snap.WorkflowStep, initialLayer)
	}
}

func TestHandleOnboardingTurn_ApprovalBeforeAnswerSendsError(t *testing.T) {
	c, sink := newInterviewTestComponent(t)
	loopID := startOnboardingLoop(c, "coby", "s1", operatingmodel.LayerOperatingRhythms)
	// Force sub-state to awaiting_approval but leave no draft (programmer
	// error / corrupted metadata). The handler should respond with a clear
	// error instead of panicking or advancing.
	updateMetadata(c.loopTracker, loopID, map[string]any{
		OnboardMetaSubState: SubStateAwaitingApproval,
	})

	c.handleOnboardingTurn(context.Background(),
		interviewUserMsg("coby", "s1", "approve"))

	rs := sink.all()
	if len(rs) != 1 || rs[0].Type != agentic.ResponseTypeError {
		t.Errorf("expected 1 error response; got %+v", rs)
	}
	info := c.loopTracker.Get(loopID)
	if info.WorkflowStep != operatingmodel.LayerOperatingRhythms {
		t.Errorf("WorkflowStep advanced despite missing draft: %q", info.WorkflowStep)
	}
}

// -- helpers --

func TestIsApprovalText(t *testing.T) {
	positives := []string{"approve", "APPROVE", "  Yes ", "y", "ok", "okay", "accept", "save"}
	for _, p := range positives {
		if !isApprovalText(p) {
			t.Errorf("isApprovalText(%q) = false, want true", p)
		}
	}
	negatives := []string{"", "maybe", "approximately", "sure i guess", "ok sure"}
	for _, n := range negatives {
		if isApprovalText(n) {
			t.Errorf("isApprovalText(%q) = true, want false", n)
		}
	}
}

func TestNextLayerAfter(t *testing.T) {
	cases := []struct {
		in, want string
		hasNext  bool
	}{
		{operatingmodel.LayerOperatingRhythms, operatingmodel.LayerRecurringDecisions, true},
		{operatingmodel.LayerRecurringDecisions, operatingmodel.LayerDependencies, true},
		{operatingmodel.LayerDependencies, operatingmodel.LayerInstitutionalKnowledge, true},
		{operatingmodel.LayerInstitutionalKnowledge, operatingmodel.LayerFriction, true},
		{operatingmodel.LayerFriction, "", false},
		{"not_a_layer", "", false},
	}
	for _, tc := range cases {
		got, ok := nextLayerAfter(tc.in)
		if ok != tc.hasNext || got != tc.want {
			t.Errorf("nextLayerAfter(%q) = (%q, %v), want (%q, %v)",
				tc.in, got, ok, tc.want, tc.hasNext)
		}
	}
}

func TestNormalizeLayerAnswer_StubShape(t *testing.T) {
	entries := NormalizeLayerAnswer(operatingmodel.LayerOperatingRhythms,
		"Weekly planning\nBlock Mondays")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Title != "Weekly planning" {
		t.Errorf("Title = %q, want %q", entries[0].Title, "Weekly planning")
	}
	if entries[0].Summary == "" {
		t.Errorf("Summary should carry full text, got empty")
	}
	if entries[0].EntryID == "" || strings.Contains(entries[0].EntryID, ".") {
		t.Errorf("EntryID = %q, want non-empty and dot-free", entries[0].EntryID)
	}
}

func TestDraftEntriesFromMetadata_RoundTripThroughAny(t *testing.T) {
	// Simulates a Metadata that has been through a JSON round-trip (values
	// become []any). The draft accessor should still recover typed entries.
	meta := map[string]any{
		OnboardMetaDraftEntries: []any{
			map[string]any{
				"entry_id": "e1",
				"title":    "t",
				"summary":  "s",
				"status":   "active",
			},
		},
	}
	entries, ok := draftEntriesFromMetadata(meta)
	if !ok || len(entries) != 1 {
		t.Fatalf("round-trip recovery failed: %+v ok=%v", entries, ok)
	}
	if entries[0].Title != "t" || entries[0].Summary != "s" {
		t.Errorf("round-tripped entry fields mismatch: %+v", entries[0])
	}
}
