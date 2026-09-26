package agentic

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/test/e2e/harness/processbarrier"
)

func TestComposeProcessControllerTargetsOnlySemStreams(t *testing.T) {
	var calls [][]string
	controller := composeProcessController{
		composeFile: "docker/compose/agentic.yml",
		service:     "semstreams",
		run: func(_ context.Context, name string, args ...string) error {
			calls = append(calls, append([]string{name}, args...))
			return nil
		},
	}

	if err := controller.kill(t.Context()); err != nil {
		t.Fatalf("kill() error = %v", err)
	}
	if err := controller.start(t.Context()); err != nil {
		t.Fatalf("start() error = %v", err)
	}

	want := [][]string{
		{"docker", "compose", "-f", "docker/compose/agentic.yml", "kill", "-s", "SIGKILL", "semstreams"},
		{"docker", "compose", "-f", "docker/compose/agentic.yml", "up", "-d", "--wait", "--no-deps", "semstreams"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("commands = %#v, want %#v", calls, want)
	}
}

func TestHarnessFinalizationIsDetachedBoundedAndJoined(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	cancel()
	primaryErr := errors.New("scenario failed")
	cleanupErr := errors.New("cleanup failed")
	runErr := error(primaryErr)

	joinHarnessFinalizationError(parent, &runErr, "restore fixture", func(finalCtx context.Context) error {
		if err := finalCtx.Err(); err != nil {
			t.Fatalf("finalization inherited cancellation: %v", err)
		}
		deadline, ok := finalCtx.Deadline()
		if !ok {
			t.Fatal("finalization context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > harnessFinalizationTimeout {
			t.Fatalf("finalization deadline remaining = %v", remaining)
		}
		return cleanupErr
	})

	if !errors.Is(runErr, primaryErr) || !errors.Is(runErr, cleanupErr) {
		t.Fatalf("joined error = %v, want primary and cleanup causes", runErr)
	}
}

func TestFirstBackOffEvidenceExcludesStartupAndSemanticRetry(t *testing.T) {
	base := time.Unix(100, 0).UTC()
	first := processbarrier.Attempt{ProcessInstance: "old", EnteredAt: base}
	tests := []struct {
		name        string
		replacement processbarrier.Attempt
		redelivery  processbarrier.Attempt
		wantErr     bool
	}{
		{
			name:        "15 second server backoff",
			replacement: processbarrier.Attempt{ProcessInstance: "new", EnteredAt: base.Add(8 * time.Second)},
			redelivery:  processbarrier.Attempt{ProcessInstance: "new", EnteredAt: base.Add(15 * time.Second)},
		},
		{
			name:        "30 second semantic retry",
			replacement: processbarrier.Attempt{ProcessInstance: "new", EnteredAt: base.Add(8 * time.Second)},
			redelivery:  processbarrier.Attempt{ProcessInstance: "new", EnteredAt: base.Add(30 * time.Second)},
			wantErr:     true,
		},
		{
			name:        "startup contaminates measurement",
			replacement: processbarrier.Attempt{ProcessInstance: "new", EnteredAt: base.Add(13 * time.Second)},
			redelivery:  processbarrier.Attempt{ProcessInstance: "new", EnteredAt: base.Add(15 * time.Second)},
			wantErr:     true,
		},
		{
			name:        "same process",
			replacement: processbarrier.Attempt{ProcessInstance: "old", EnteredAt: base.Add(8 * time.Second)},
			redelivery:  processbarrier.Attempt{ProcessInstance: "old", EnteredAt: base.Add(15 * time.Second)},
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateFirstBackOffEvidence(first, tt.replacement, tt.redelivery)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateFirstBackOffEvidence() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBarrierReleaseFlushDerivesBoundedOperationContext(t *testing.T) {
	parent := t.Context()
	if _, ok := parent.Deadline(); ok {
		t.Fatal("test requires the same no-deadline context shape as the E2E runner")
	}
	called := false
	err := flushBarrierRelease(parent, func(flushCtx context.Context) error {
		called = true
		deadline, ok := flushCtx.Deadline()
		if !ok {
			t.Fatal("flush context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > barrierReleaseFlushTimeout {
			t.Fatalf("flush deadline remaining = %v, want (0, %v]", remaining, barrierReleaseFlushTimeout)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("flushBarrierRelease() error = %v", err)
	}
	if !called {
		t.Fatal("flush callback was not called")
	}
}

func TestComposeProcessControllerRejectsIncompleteTarget(t *testing.T) {
	for _, controller := range []composeProcessController{
		{service: "semstreams", run: func(context.Context, string, ...string) error { return nil }},
		{composeFile: "agentic.yml", run: func(context.Context, string, ...string) error { return nil }},
		{composeFile: "agentic.yml", service: "semstreams"},
	} {
		if err := controller.kill(t.Context()); err == nil {
			t.Fatalf("kill() accepted incomplete controller: %#v", controller)
		}
	}
}

// The W-b assertions of the mid-flight replacement check (#1365, task 3.7)
// refuse each wrong shape the tier could observe, and name it.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestDeferredTurnCarriedOnceRefusesEveryWrongCount(t *testing.T) {
	const (
		loop  = "d4e5f607-1829-4a3b-8c4d-5e6f70819203"
		birth = "Analyze the temperature sensor."
		want  = loop + ":req:2:0"
	)
	turn := deferredTurnPrompt(loop)
	user := func(content string) agentic.ChatMessage { return agentic.ChatMessage{Role: "user", Content: content} }
	call := agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{{ID: "call_1", Name: "query_entity"}}}
	answer := agentic.ChatMessage{Role: "tool", ToolCallID: "call_1", Content: "{}"}

	tests := []struct {
		name     string
		request  *agentic.AgentRequest
		wantFail string
	}{
		{"carried once after the birth prompt", &agentic.AgentRequest{RequestID: want,
			Messages: []agentic.ChatMessage{user(birth), user(turn), call, answer}}, ""},
		{"lost", &agentic.AgentRequest{RequestID: want,
			Messages: []agentic.ChatMessage{user(birth), call, answer}}, "in 0 user messages"},
		{"replayed over a carrier", &agentic.AgentRequest{RequestID: want,
			Messages: []agentic.ChatMessage{user(birth), user(turn), user(turn), call, answer}}, "in 2 user messages"},
		{"ahead of the conversation", &agentic.AgentRequest{RequestID: want,
			Messages: []agentic.ChatMessage{user(turn), user(birth), call, answer}}, "before R1's conversation"},
		{"birth prompt seated twice", &agentic.AgentRequest{RequestID: want,
			Messages: []agentic.ChatMessage{user(birth), user(birth), user(turn), call, answer}}, "birth prompt in 2"},
		{"another request", &agentic.AgentRequest{RequestID: loop + ":req:1:0",
			Messages: []agentic.ChatMessage{user(birth), user(turn)}}, "next request id"},
		// The 4a4e070e tier red: compaction summarized the turn into a system
		// message. Quoted there, it is not carried — only a user message counts.
		{"quoted only in a compaction summary", &agentic.AgentRequest{RequestID: want,
			Messages: []agentic.ChatMessage{user(birth), {Role: "system", Content: "Summary so far: " + turn}, call, answer}},
			"in 0 user messages"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkDeferredTurnCarriedOnce(tt.request, want, birth, turn)
			assertCheck(t, err, tt.wantFail)
		})
	}
}

// spec: agentic-loop / The loop record names its outstanding request
func TestCompletionPromptIsTheBirthPrompt(t *testing.T) {
	const birth = "Analyze the temperature sensor."
	turn := deferredTurnPrompt("d4e5f607-1829-4a3b-8c4d-5e6f70819203")
	tests := []struct {
		name     string
		payload  message.Payload
		wantFail string
	}{
		{"birth prompt", &agentic.LoopCompletedEvent{Prompt: birth}, ""},
		{"no prompt", &agentic.LoopCompletedEvent{}, "carries no prompt"},
		{"the continuation's turn", &agentic.LoopCompletedEvent{Prompt: turn}, "carries the continuation's turn"},
		{"another prompt", &agentic.LoopCompletedEvent{Prompt: "something else"}, "want the birth prompt"},
		{"a failure event", &agentic.LoopFailedEvent{Prompt: birth}, "want *agentic.LoopCompletedEvent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertCheck(t, checkCompletionPrompt(tt.payload, birth, turn), tt.wantFail)
		})
	}
}

func assertCheck(t *testing.T, err error, wantFail string) {
	t.Helper()
	if wantFail == "" {
		if err != nil {
			t.Fatalf("check refused a correct observation: %v", err)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), wantFail) {
		t.Fatalf("check error = %v, want one naming %q", err, wantFail)
	}
}
