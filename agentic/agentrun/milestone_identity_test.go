package agentrun_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/agentic/agentrun"
	"github.com/c360studio/semstreams/message"
)

// TestMilestoneFanoutPresentsSameSourceMessageIDOnEveryAttempt is the handler-done
// contract's precondition (design § 2.1/2.2, invariant I2): a handler can only make
// its durable consequence idempotent if every attempt of one stored delivery hands
// it the same key.
//
// The two attempts are the same stored BYTES replayed, which is exactly what
// JetStream redelivers — the identity therefore has to come off the wire envelope
// and not from anything the subscriber computes per attempt.
func TestMilestoneFanoutPresentsSameSourceMessageIDOnEveryAttempt(t *testing.T) {
	t.Parallel()
	reader := newFakeTripleReader()
	mock := newMockLifecycleManager()

	var seen []string
	sub := agentrun.NewMilestoneSubscriberWithRunStateReader(mock, reader, "acme", "ops", nil)
	sub.AddHandler(&testMilestoneHandler{
		fn: func(_ context.Context, ev agentrun.LoopTerminalEvent, _ *agentrun.AgentRun) error {
			seen = append(seen, ev.SourceMessageID)
			return nil
		},
	})

	completed := &agentic.LoopCompletedEvent{
		LoopID:      "replayed-loop",
		TaskID:      "task-replay",
		Outcome:     agentic.OutcomeSuccess,
		Role:        "researcher",
		CompletedAt: time.Now().UTC(),
	}
	envelope := message.NewBaseMessage(completed.Schema(), completed, "agentic-loop")
	wantID := envelope.ID()
	require.NotEmpty(t, wantID, "the production envelope must carry a message identity")
	data, err := json.Marshal(envelope)
	require.NoError(t, err)

	for attempt := 1; attempt <= 2; attempt++ {
		require.NoError(t, sub.HandleEvent(context.Background(), data),
			"attempt %d of a replay-safe delivery", attempt)
	}

	require.Len(t, seen, 2, "every attempt invokes the handler")
	assert.Equal(t, wantID, seen[0], "attempt 1 must present the wire message identity")
	assert.Equal(t, wantID, seen[1], "attempt 2 must present the SAME identity, not a fresh one")
}
