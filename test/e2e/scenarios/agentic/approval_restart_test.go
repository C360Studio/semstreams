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
			wantErr: "task prompt",
		},
		{
			name: "conversation not rebuilt: no gated call",
			request: &agentic.AgentRequest{RequestID: wantID, Messages: []agentic.ChatMessage{
				{Role: "user", Content: restartTestPrompt},
				executedAnswer(),
			}},
			answer:  approve,
			wantErr: "no assistant turn",
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

func TestDeadlinesAheadRefusesAGapThatOutranEitherDeadline(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	gate := restartTestGate()
	gate.RequestedAt = now.Add(-time.Minute)
	gate.Timeout = 5 * time.Minute

	parked := agentic.LoopEntity{TimeoutAt: now.Add(time.Minute), PendingApproval: &gate}
	if err := deadlinesAhead(parked, now); err != nil {
		t.Fatalf("both deadlines ahead: error = %v, want nil", err)
	}

	loopExpired := parked
	loopExpired.TimeoutAt = now
	if err := deadlinesAhead(loopExpired, now); err == nil || !strings.Contains(err.Error(), "agentic-loop.timeout") {
		t.Fatalf("loop deadline passed: error = %v, want a refusal naming agentic-loop.timeout", err)
	}

	expiredGate := gate
	expiredGate.RequestedAt = now.Add(-gate.Timeout)
	approvalExpired := parked
	approvalExpired.PendingApproval = &expiredGate
	if err := deadlinesAhead(approvalExpired, now); err == nil ||
		!strings.Contains(err.Error(), "agentic-loop.approval_timeout") {
		t.Fatalf("approval deadline passed: error = %v, want a refusal naming agentic-loop.approval_timeout", err)
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

// TestApprovalRestartLanesCoverEveryLoopInputThatCanSeatAParkedLoop pins the
// quiescence set. Dropping a lane would let its redelivery reach the
// replacement ahead of the answer and seat the loop warm, and the stage would
// then pass without the cold branch ever running.
func TestApprovalRestartLanesCoverEveryLoopInputThatCanSeatAParkedLoop(t *testing.T) {
	want := map[consumerLane]bool{
		{stream: agentStream, owner: loopLaneOwner, subjectRoot: "agent.task"}:              true,
		{stream: agentStream, owner: loopLaneOwner, subjectRoot: "agent.response"}:          true,
		{stream: agentStream, owner: loopLaneOwner, subjectRoot: "agent.approval_response"}: true,
		{stream: toolStream, owner: loopLaneOwner, subjectRoot: "tool.result"}:              true,
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
