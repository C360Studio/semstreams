package agentic

import (
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	restartTestLoop   = "c3d4e5f6-0718-4920-9c3d-4e5f60718293"
	restartTestPrompt = "List the agent execution entities on record. Use the query_by_type tool."
	restartTestCall   = "call_abc12345"
)

func restartTestGate() agentic.PendingApprovalState {
	return agentic.PendingApprovalState{
		RequestID:   restartTestLoop + ":req:1:0",
		ExecutionID: "exec-1",
		CallID:      restartTestCall,
		ToolName:    approvalGatedTool,
	}
}

// restartTestRequest is the next request a rebuilt loop publishes: the task
// prompt, the assistant turn that made the gated call, and the given tool
// messages answering it.
func restartTestRequest(answers ...agentic.ChatMessage) *agentic.AgentRequest {
	messages := []agentic.ChatMessage{
		{Role: "user", Content: restartTestPrompt},
		{Role: "assistant", ToolCalls: []agentic.ToolCall{{ID: restartTestCall, Name: approvalGatedTool}}},
	}
	messages = append(messages, answers...)
	return &agentic.AgentRequest{RequestID: restartTestLoop + ":req:2:0", Messages: messages}
}

func executedAnswer() agentic.ChatMessage {
	return agentic.ChatMessage{Role: "tool", ToolCallID: restartTestCall, Content: `{"matched":1}`}
}

func rejectionAnswer(approver, reason string) agentic.ChatMessage {
	return agentic.ChatMessage{
		Role:       "tool",
		ToolCallID: restartTestCall,
		IsError:    true,
		Content:    "Tool error: " + agentic.ApprovalRejectedPrefix + "rejected by " + approver + ": " + reason,
	}
}

func placeholderAnswer() agentic.ChatMessage {
	return agentic.ChatMessage{
		Role:       "tool",
		ToolCallID: restartTestCall,
		IsError:    true,
		Content:    "Tool error: " + agentic.ApprovalRequiredPrefix + "query_by_type requires approval",
	}
}

// TestCheckAppliedAnswerTellsTheAnswerFromItsImpostors holds the stage's one
// content assertion against each shape that could pass for "the replacement
// applied the answer" without being it.
func TestCheckAppliedAnswerTellsTheAnswerFromItsImpostors(t *testing.T) {
	approve := approvalAnswer{decision: agentic.ApprovalDecisionApprove, approver: approvalRequester}
	reject := approvalAnswer{
		decision: agentic.ApprovalDecisionReject, approver: approvalRequester, reason: approvalRestartRejectReason,
	}
	wantID := restartTestLoop + ":req:2:0"

	tests := []struct {
		name    string
		request *agentic.AgentRequest
		answer  approvalAnswer
		wantErr string
	}{
		{name: "approved call executed", request: restartTestRequest(executedAnswer()), answer: approve},
		{
			name:    "rejection applied",
			request: restartTestRequest(rejectionAnswer(approvalRequester, approvalRestartRejectReason)),
			answer:  reject,
		},
		{
			name:    "approval answered by a rejection",
			request: restartTestRequest(rejectionAnswer(approvalRequester, approvalRestartRejectReason)),
			answer:  approve,
			wantErr: "is an error",
		},
		{
			name:    "approval where only the placeholder reached the model",
			request: restartTestRequest(placeholderAnswer()),
			answer:  approve,
			wantErr: "is an error",
		},
		{
			name: "the approval-timeout sweeper decided instead of the human",
			request: restartTestRequest(rejectionAnswer(
				"system:approval-timeout", "approval timeout exceeded")),
			answer:  reject,
			wantErr: "not another rejection's",
		},
		{
			name:    "rejection that executed the call",
			request: restartTestRequest(executedAnswer()),
			answer:  reject,
			wantErr: "not another rejection's",
		},
		{
			name:    "placeholder leaked beside the answer",
			request: restartTestRequest(placeholderAnswer(), executedAnswer()),
			answer:  approve,
			wantErr: "want exactly 1",
		},
		{
			name:    "no answer for the gated call",
			request: restartTestRequest(),
			answer:  approve,
			wantErr: "want exactly 1",
		},
		{
			name: "conversation not rebuilt: no task prompt",
			request: &agentic.AgentRequest{RequestID: wantID, Messages: []agentic.ChatMessage{
				{Role: "assistant", ToolCalls: []agentic.ToolCall{{ID: restartTestCall}}},
				executedAnswer(),
			}},
			answer:  approve,
			wantErr: "task prompt in 0 user messages",
		},
		{
			name: "conversation not rebuilt: no gated call",
			request: &agentic.AgentRequest{RequestID: wantID, Messages: []agentic.ChatMessage{
				{Role: "user", Content: restartTestPrompt},
				executedAnswer(),
			}},
			answer:  approve,
			wantErr: "0 assistant turns",
		},
		{
			name: "conversation rebuilt twice: the gated assistant turn duplicated",
			request: &agentic.AgentRequest{RequestID: wantID, Messages: []agentic.ChatMessage{
				{Role: "user", Content: restartTestPrompt},
				{Role: "assistant", ToolCalls: []agentic.ToolCall{{ID: restartTestCall, Name: approvalGatedTool}}},
				{Role: "assistant", ToolCalls: []agentic.ToolCall{{ID: restartTestCall, Name: approvalGatedTool}}},
				executedAnswer(),
			}},
			answer:  approve,
			wantErr: "2 assistant turns",
		},
		{
			name: "conversation rebuilt twice: the task prompt duplicated",
			request: &agentic.AgentRequest{RequestID: wantID, Messages: []agentic.ChatMessage{
				{Role: "user", Content: restartTestPrompt},
				{Role: "user", Content: restartTestPrompt},
				{Role: "assistant", ToolCalls: []agentic.ToolCall{{ID: restartTestCall, Name: approvalGatedTool}}},
				executedAnswer(),
			}},
			answer:  approve,
			wantErr: "task prompt in 2 user messages",
		},
		{
			name:    "a different request",
			request: &agentic.AgentRequest{RequestID: restartTestLoop + ":req:1:0"},
			answer:  approve,
			wantErr: "next request id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkAppliedAnswer(tt.request, wantID, restartTestPrompt, restartTestGate(), tt.answer)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("checkAppliedAnswer() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("checkAppliedAnswer() error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

// TestPublishedBeforeWrittenRefusesTheWriteFirstOrder holds the order check
// against both orders and against a history that never reached the effect.
func TestPublishedBeforeWrittenRefusesTheWriteFirstOrder(t *testing.T) {
	base := time.Unix(1_000, 0).UTC()
	next := restartTestLoop + ":req:2:0"
	namesNext := func(entity agentic.LoopEntity) bool { return entity.PublishedRequestID == next }
	history := func(advancedAt time.Time) []loopRevision {
		return []loopRevision{
			{entity: agentic.LoopEntity{PublishedRequestID: restartTestLoop + ":req:1:0"}, revision: 7, committed: base},
			{entity: agentic.LoopEntity{PublishedRequestID: next}, revision: 9, committed: advancedAt},
			{entity: agentic.LoopEntity{PublishedRequestID: next, State: agentic.LoopStateComplete},
				revision: 11, committed: advancedAt.Add(time.Second)},
		}
	}
	published := base.Add(2 * time.Second)

	if err := publishedBeforeWritten(published, history(published.Add(time.Millisecond)), namesNext); err != nil {
		t.Fatalf("publish then write: error = %v, want nil", err)
	}
	// The FIRST matching revision decides: the terminal revision after the
	// publication must not rescue an advance committed before it.
	err := publishedBeforeWritten(published, history(published.Add(-time.Millisecond)), namesNext)
	if err == nil || !strings.Contains(err.Error(), "revision 9") {
		t.Fatalf("write then publish: error = %v, want a refusal naming revision 9", err)
	}
	err = publishedBeforeWritten(published, history(published)[:1], namesNext)
	if err == nil || !strings.Contains(err.Error(), "no watched record revision") {
		t.Fatalf("no advance: error = %v, want a refusal", err)
	}
}

func TestDeadlinesAheadRefusesAGapThatLeftEitherDeadlineUnderTheMargin(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	gate := restartTestGate()
	gate.RequestedAt = now.Add(-time.Minute)
	gate.Timeout = 5 * time.Minute

	parked := agentic.LoopEntity{TimeoutAt: now.Add(time.Minute), PendingApproval: &gate}
	if err := deadlinesAhead(parked, now); err != nil {
		t.Fatalf("both deadlines a minute or more ahead: error = %v, want nil", err)
	}

	atMargin := parked
	atMargin.TimeoutAt = now.Add(approvalRestartDeadlineMargin)
	if err := deadlinesAhead(atMargin, now); err != nil {
		t.Fatalf("loop deadline exactly the margin ahead: error = %v, want nil", err)
	}

	// Still ahead, but under the margin: the deadline would fire while the
	// answer is being applied.
	loopShort := parked
	loopShort.TimeoutAt = now.Add(approvalRestartDeadlineMargin - time.Millisecond)
	if err := deadlinesAhead(loopShort, now); err == nil || !strings.Contains(err.Error(), "agentic-loop.timeout") ||
		!strings.Contains(err.Error(), approvalRestartDeadlineMargin.String()+" margin") {
		t.Fatalf("loop deadline under the margin: error = %v, want a refusal naming agentic-loop.timeout and the margin",
			err)
	}

	shortGate := gate
	shortGate.RequestedAt = now.Add(approvalRestartDeadlineMargin - gate.Timeout - time.Millisecond)
	approvalShort := parked
	approvalShort.PendingApproval = &shortGate
	if err := deadlinesAhead(approvalShort, now); err == nil ||
		!strings.Contains(err.Error(), "agentic-loop.approval_timeout") {
		t.Fatalf("approval deadline under the margin: error = %v, want a refusal naming agentic-loop.approval_timeout",
			err)
	}

	unbounded := gate
	unbounded.Timeout = 0
	unbounded.RequestedAt = now.Add(-24 * time.Hour)
	waitsIndefinitely := parked
	waitsIndefinitely.PendingApproval = &unbounded
	if err := deadlinesAhead(waitsIndefinitely, now); err != nil {
		t.Fatalf("zero approval timeout waits indefinitely: error = %v, want nil", err)
	}
}

// TestBudgetLeftTellsASpentBudgetFromARemainingOne holds the report every wait
// error after the answer carries: a deadline that passed during the wait must
// read as spent, so the error blames the budget and not the answer.
func TestBudgetLeftTellsASpentBudgetFromARemainingOne(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	gate := restartTestGate()
	gate.RequestedAt = now.Add(-time.Minute)
	gate.Timeout = 5 * time.Minute
	parked := agentic.LoopEntity{TimeoutAt: now.Add(time.Minute), PendingApproval: &gate}

	budget, spent := budgetLeft(parked, now)
	if spent || !strings.Contains(budget, "agentic-loop.timeout) 1m0s left") ||
		!strings.Contains(budget, "agentic-loop.approval_timeout) 4m0s left") {
		t.Fatalf("budgetLeft(both ahead) = %q, %v; want both named with their time left, not spent", budget, spent)
	}

	loopPassed := parked
	loopPassed.TimeoutAt = now.Add(-2 * time.Second)
	budget, spent = budgetLeft(loopPassed, now)
	if !spent || !strings.Contains(budget, "agentic-loop.timeout) passed 2s ago") {
		t.Fatalf("budgetLeft(loop deadline passed) = %q, %v; want it named as passed, and spent", budget, spent)
	}

	exactlyNow := parked
	exactlyNow.TimeoutAt = now
	if _, spent := budgetLeft(exactlyNow, now); !spent {
		t.Fatal("budgetLeft(loop deadline exactly now) is not spent; a deadline at now has fired")
	}

	if budget, spent := budgetLeft(agentic.LoopEntity{}, now); spent || budget != "no deadline set" {
		t.Fatalf("budgetLeft(no deadlines) = %q, %v; want no deadline set, not spent", budget, spent)
	}
}

// TestWatchSkippedNothingRefusesAHistoryItCannotProveComplete holds the proof
// that the watched revisions are every revision of the key between the parked
// one and the terminal one.
func TestWatchSkippedNothingRefusesAHistoryItCannotProveComplete(t *testing.T) {
	watched := func(revisions ...uint64) []loopRevision {
		out := make([]loopRevision, 0, len(revisions))
		for _, revision := range revisions {
			out = append(out, loopRevision{revision: revision})
		}
		return out
	}
	// The key's revisions interleave with other keys' in the bucket's stream,
	// so they are not consecutive integers.
	retained := []uint64{3, 12, 20, 21, 27, 31}
	const parked, through = 12, 27

	tests := []struct {
		name     string
		watched  []loopRevision
		retained []uint64
		wantErr  string
	}{
		{name: "every revision from parked to terminal", watched: watched(12, 20, 21, 27), retained: retained},
		{
			name: "a revision after the terminal is outside the window", watched: watched(12, 20, 21, 27),
			retained: []uint64{12, 20, 21, 27, 31},
		},
		{name: "a skipped revision", watched: watched(12, 21, 27), retained: retained, wantErr: "delivered revisions"},
		{
			name: "the watch began after the parked revision", watched: watched(20, 21, 27), retained: retained,
			wantErr: "not the parked revision 12",
		},
		{name: "the watch delivered nothing", retained: retained, wantErr: "began at nothing"},
		{
			name: "the retained history no longer reaches the parked revision", watched: watched(12, 20, 21, 27),
			retained: []uint64{20, 21, 27}, wantErr: "no longer reaches",
		},
		{
			name: "a revision the key never retained", watched: watched(12, 20, 25, 21, 27), retained: retained,
			wantErr: "delivered revisions",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := watchSkippedNothing(tt.watched, tt.retained, parked, through)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("watchSkippedNothing() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("watchSkippedNothing() error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestFiltersUnderMatchesTheLaneAndNotItsNeighbours(t *testing.T) {
	tests := []struct {
		name   string
		config jetstream.ConsumerConfig
		root   string
		want   bool
	}{
		{"single filter under root", jetstream.ConsumerConfig{FilterSubject: "tool.result.>"}, "tool.result", true},
		{"filter is the root", jetstream.ConsumerConfig{FilterSubject: "agent.task"}, "agent.task", true},
		{"one of several filters", jetstream.ConsumerConfig{
			FilterSubjects: []string{"agent.signal.*", "agent.approval_response.*"},
		}, "agent.approval_response", true},
		{"a sibling sharing the prefix text", jetstream.ConsumerConfig{FilterSubject: "agent.tasks.>"}, "agent.task", false},
		{"another lane", jetstream.ConsumerConfig{FilterSubject: "agent.response.>"}, "agent.task", false},
		{"no filter", jetstream.ConsumerConfig{}, "agent.task", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := filtersUnder(tt.config, tt.root); got != tt.want {
				t.Fatalf("filtersUnder(%+v, %q) = %v, want %v", tt.config.FilterSubjects, tt.root, got, tt.want)
			}
		})
	}
}

// TestApprovalRestartLanesPinTheQuiescenceSet pins the hand-enumerated
// quiescence set; approvalRestartLanes carries the enumeration and why each
// lane is in or out. The pin catches a lane dropped or changed without this
// test changing with it. It cannot catch a loop input the enumeration never
// listed: a new port in the loop's routing, or a new upstream producer, must
// be added to both by hand. Dropping a lane would let its redelivery reach the
// replacement ahead of the answer and seat the loop warm, and the stage would
// then pass without the cold branch ever running.
func TestApprovalRestartLanesPinTheQuiescenceSet(t *testing.T) {
	want := map[consumerLane]bool{
		{stream: agentStream, owner: loopLaneOwner, subjectRoot: "agent.task"}:              true,
		{stream: agentStream, owner: loopLaneOwner, subjectRoot: "agent.response"}:          true,
		{stream: agentStream, owner: loopLaneOwner, subjectRoot: "agent.approval_response"}: true,
		{stream: toolStream, owner: loopLaneOwner, subjectRoot: "tool.result"}:              true,
		{stream: agentStream, owner: modelLaneOwner, subjectRoot: "agent.request"}:          true,
		{stream: toolStream, owner: toolsLaneOwner, subjectRoot: "tool.execute"}:            true,
	}
	if len(approvalRestartLanes) != len(want) {
		t.Fatalf("approvalRestartLanes = %v, want %d lanes", approvalRestartLanes, len(want))
	}
	for _, lane := range approvalRestartLanes {
		if !want[lane] {
			t.Errorf("unexpected lane %+v", lane)
		}
		delete(want, lane)
	}
	for lane := range want {
		t.Errorf("missing lane %+v", lane)
	}
}
