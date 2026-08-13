package contract

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/agentic/agentrun"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/lifecycle"
)

type terminalCompatAbsentRunReader struct{}

func (terminalCompatAbsentRunReader) Get(context.Context, string, string) (lifecycle.Participant, error) {
	return nil, lifecycle.ErrEntityNotFound
}

type terminalCompatCapture struct{ events []agentrun.LoopTerminalEvent }

func (c *terminalCompatCapture) OnLoopTerminal(
	_ context.Context,
	event agentrun.LoopTerminalEvent,
	_ *agentrun.AgentRun,
) error {
	c.events = append(c.events, event)
	return nil
}

func TestLocalSemstreamsAgentRunProductionTerminalCallbacks(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	subscriber := agentrun.NewMilestoneSubscriberWithRunStateReader(
		terminalCompatAbsentRunReader{}, nil, "semteams", "test", logger,
	)
	capture := &terminalCompatCapture{}
	subscriber.AddHandler(capture)
	at := time.Unix(1_700_001_000, 0).UTC()
	payloads := []message.Payload{
		&agentic.LoopCompletedEvent{
			LoopID: "loop-success", TaskID: "task-success", RunEntityID: "missing-success",
			Outcome: agentic.OutcomeSuccess, CompletedAt: at,
		},
		&agentic.LoopFailedEvent{
			LoopID: "loop-failed", TaskID: "task-failed", RunEntityID: "missing-failed",
			Outcome: agentic.OutcomeFailed, FailedAt: at,
		},
		&agentic.LoopCancelledEvent{
			LoopID: "loop-cancelled", TaskID: "task-cancelled", RunEntityID: "missing-cancelled",
			Outcome: agentic.OutcomeCancelled, CancelledAt: at,
		},
	}

	for _, payload := range payloads {
		data, err := json.Marshal(message.NewBaseMessage(payload.Schema(), payload, "agentic-loop"))
		if err != nil {
			t.Fatalf("marshal %s: %v", payload.Schema().Category, err)
		}
		if err := subscriber.HandleEvent(context.Background(), data); err != nil {
			t.Fatalf("HandleEvent %s: %v", payload.Schema().Category, err)
		}
	}

	if len(capture.events) != 3 {
		t.Fatalf("callbacks = %d, want 3", len(capture.events))
	}
	wantCategories := []string{
		agentic.CategoryLoopCompleted, agentic.CategoryLoopFailed, agentic.CategoryLoopCancelled,
	}
	wantOutcomes := []string{agentic.OutcomeSuccess, agentic.OutcomeFailed, agentic.OutcomeCancelled}
	for i := range capture.events {
		if capture.events[i].Category != wantCategories[i] || capture.events[i].Outcome != wantOutcomes[i] {
			t.Fatalf("callback %d = %s/%s, want %s/%s", i,
				capture.events[i].Category, capture.events[i].Outcome, wantCategories[i], wantOutcomes[i])
		}
	}
}
