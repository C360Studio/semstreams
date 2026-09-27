//go:build integration

package agenticloop

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// faultyUpdateBucket passes every call to the real loops bucket except
// Update, which returns fault while one is set.
type faultyUpdateBucket struct {
	jetstream.KeyValue
	fault atomic.Pointer[error]
}

func (b *faultyUpdateBucket) Update(ctx context.Context, key string, value []byte, revision uint64) (uint64, error) {
	if fault := b.fault.Load(); fault != nil {
		return 0, *fault
	}
	return b.KeyValue.Update(ctx, key, value, revision)
}

func (b *faultyUpdateBucket) failWith(err error) { b.fault.Store(&err) }
func (b *faultyUpdateBucket) heal()              { b.fault.Store(nil) }

// lockedLog is a log sink the delivery's goroutines may write concurrently.
type lockedLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// deferredWriteStage is a real loop with its first request outstanding, so a
// continuation for it defers, and a loops bucket whose Update can be failed.
type deferredWriteStage struct {
	component *Component
	handler   *MessageHandler
	bucket    *faultyUpdateBucket
	logs      *lockedLog
	loopID    string
}

func startDeferredWriteStage(t *testing.T, client *natsclient.Client, loopID string) deferredWriteStage {
	t.Helper()
	c, h := startLoopProcess(t, client, DefaultConfig())
	logs := &lockedLog{}
	c.logger = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	_, birth := deliverTask(t, c, agentic.TaskMessage{
		TaskID: "task-birth-" + loopID[:8], LoopID: loopID, Role: "general", Model: "test-model",
		Prompt: "the task that bore the loop",
	})
	require.Equal(t, natsclient.DeliveryDecisionAck, birth.Decision())
	bucket := &faultyUpdateBucket{KeyValue: c.loopsBucket}
	c.loopsBucket = bucket
	return deferredWriteStage{component: c, handler: h, bucket: bucket, logs: logs, loopID: loopID}
}

func (s deferredWriteStage) turn(taskID, prompt string) agentic.TaskMessage {
	return agentic.TaskMessage{TaskID: taskID, LoopID: s.loopID, Role: "general", Model: "test-model", Prompt: prompt}
}

// timesInContext counts the user turns in the live conversation that say prompt.
func (s deferredWriteStage) timesInContext(prompt string) int {
	n := 0
	for _, m := range s.handler.loopManager.GetContextManager(s.loopID).GetContext() {
		if m.Role == "user" && m.Content == prompt {
			n++
		}
	}
	return n
}

// TestADeferredTurnWhoseWriteFailsIsRetried is #1400 (owner ruling
// Q3 on #1146, issuecomment-5854830449): a deferred turn's durable write that
// fails for any reason but a size refusal leaves the delivery unacknowledged,
// and the redelivery re-runs the write rather than being deduplicated away.
//
// The fault's text names a disk-full condition on purpose: errs.IsFatal
// matches that text, so a failure returned without an explicit Transient
// wrapper would quarantine the task lane over one loop's write.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestADeferredTurnWhoseWriteFailsIsRetried(t *testing.T) {
	client := newLoopNATS(t)
	bucketDown := errors.New("nats: insufficient storage — disk full on the loops bucket")

	t.Run("a redelivery into the process that holds the loop re-runs the write", func(t *testing.T) {
		stage := startDeferredWriteStage(t, client, "3c7e1a52-9b0d-4f68-a2e4-5d1b8c9f0a37")
		const prompt = "a turn typed while the bucket was down"
		turn := stage.turn("task-deferred-retry", prompt)

		stage.bucket.failWith(bucketDown)
		msg, first := deliverTask(t, stage.component, turn)
		require.Equal(t, natsclient.DeliveryDecisionRetry, first.Decision(),
			"a failed write of the turn is never acknowledged, and never quarantines the lane")
		require.Zero(t, msg.acks.Load())
		require.Contains(t, stage.logs.String(), "Deferred turn's record write failed",
			"the retry is declared at the site")
		require.False(t, loopRecordOf(t, stage.component, stage.loopID).entity.PendingContinuation)

		stage.bucket.heal()
		_, redelivered := deliverTask(t, stage.component, turn)
		require.Equal(t, natsclient.DeliveryDecisionAck, redelivered.Decision())
		record := loopRecordOf(t, stage.component, stage.loopID).entity
		require.True(t, record.PendingContinuation, "the redelivery wrote the marker the first delivery could not")
		require.Empty(t, record.PendingContinuationRequestID)
		require.Equal(t, prompt, record.PendingContinuationPrompt, "and the turn's text with it")
		require.Equal(t, 1, stage.timesInContext(prompt), "the redelivery does not append the turn again")
	})

	t.Run("a redelivery after a later turn deferred leaves the later turn standing", func(t *testing.T) {
		stage := startDeferredWriteStage(t, client, "8f2d4b60-1e7a-4c39-b5d8-0a6c3e9f7b21")
		const first, second = "the first turn, whose write failed", "the second turn, typed during the retry delay"
		firstTurn := stage.turn("task-deferred-first", first)

		stage.bucket.failWith(bucketDown)
		_, failed := deliverTask(t, stage.component, firstTurn)
		require.Equal(t, natsclient.DeliveryDecisionRetry, failed.Decision())

		stage.bucket.heal()
		_, later := deliverTask(t, stage.component, stage.turn("task-deferred-second", second))
		require.Equal(t, natsclient.DeliveryDecisionAck, later.Decision())

		_, redelivered := deliverTask(t, stage.component, firstTurn)
		require.Equal(t, natsclient.DeliveryDecisionAck, redelivered.Decision(),
			"the first turn is no longer the loop's uncarried turn, so there is nothing of its own to write")
		require.Equal(t, 1, stage.timesInContext(first), "the redelivery must not attach the first turn a second time")
		require.Equal(t, 1, stage.timesInContext(second))
		record := loopRecordOf(t, stage.component, stage.loopID).entity
		require.True(t, record.PendingContinuation)
		require.Equal(t, second, record.PendingContinuationPrompt,
			"the record keeps the latest uncarried turn; the redelivery must not flip it back")
	})

	t.Run("a redelivery after the outstanding response carried the turn does not un-carry it", func(t *testing.T) {
		// The case production meets most: the model answers during the retry
		// delay, TrackRequest names the carrier and leaves the marker, the task
		// id and the text exactly as they were, so only the carrier clause of
		// the resume gate stops a re-write that would overlay "no carrier" onto
		// the carrier's record — and a replacement would then replay a turn the
		// carrier already sent.
		stage := startDeferredWriteStage(t, client, "c6f1e2d9-8a47-4b30-9d5c-3e7a0b1f6c28")
		const prompt = "a turn whose carrier went out during the retry delay"
		turn := stage.turn("task-deferred-carried", prompt)

		stage.bucket.failWith(bucketDown)
		_, failed := deliverTask(t, stage.component, turn)
		require.Equal(t, natsclient.DeliveryDecisionRetry, failed.Decision())

		stage.bucket.heal()
		firstRequest := looprequest.ID{LoopID: stage.loopID, Iteration: 1, Retry: 0}.String()
		completion := agentic.AgentResponse{
			RequestID: firstRequest,
			Status:    agentic.StatusComplete,
			Message:   agentic.ChatMessage{Role: "assistant", Content: "the first thing is done"},
		}
		retainModelResponse(t, client, completion)
		_, answered := deliverResponse(t, stage.component, completion)
		require.Equal(t, natsclient.DeliveryDecisionAck, answered.Decision())
		carrier := looprequest.ID{LoopID: stage.loopID, Iteration: 2, Retry: 0}.String()
		require.Equal(t, carrier, loopRecordOf(t, stage.component, stage.loopID).entity.PendingContinuationRequestID,
			"the carrier's own write names it")

		_, redelivered := deliverTask(t, stage.component, turn)
		require.Equal(t, natsclient.DeliveryDecisionAck, redelivered.Decision())
		record := loopRecordOf(t, stage.component, stage.loopID).entity
		require.Equal(t, carrier, record.PendingContinuationRequestID,
			"the redelivery must not overlay no-carrier onto a turn its carrier already sent")
		require.Equal(t, 1, stage.timesInContext(prompt))
	})

	t.Run("a loop with no observed revision retries rather than quarantining the lane", func(t *testing.T) {
		stage := startDeferredWriteStage(t, client, "b41e9c07-6d2a-4f85-9e3b-7c0a5d8f2e16")
		const prompt = "a turn deferred behind a revision this process lost"
		turn := stage.turn("task-deferred-unobserved", prompt)
		held := loopRecordOf(t, stage.component, stage.loopID)

		stage.component.forgetLoopRevision(stage.loopID)
		_, first := deliverTask(t, stage.component, turn)
		require.Equal(t, natsclient.DeliveryDecisionRetry, first.Decision())

		stage.component.rememberLoopRevision(stage.loopID, held.revision)
		_, redelivered := deliverTask(t, stage.component, turn)
		require.Equal(t, natsclient.DeliveryDecisionAck, redelivered.Decision())
		require.Equal(t, prompt, loopRecordOf(t, stage.component, stage.loopID).entity.PendingContinuationPrompt)
	})

	t.Run("a released loop takes its remembered result with it", func(t *testing.T) {
		stage := startDeferredWriteStage(t, client, "e0a73d18-4c9b-4b52-8f61-2d7e9a0c5b43")
		turn := stage.turn("task-deferred-released", "a turn whose loop was released during the delay")

		stage.bucket.failWith(bucketDown)
		_, failed := deliverTask(t, stage.component, turn)
		require.Equal(t, natsclient.DeliveryDecisionRetry, failed.Decision())
		_, remembered := stage.component.pendingTaskResult(turn.TaskID, stage.loopID)
		require.True(t, remembered)

		stage.component.releaseLoopTransientState(stage.loopID)
		_, remembered = stage.component.pendingTaskResult(turn.TaskID, stage.loopID)
		require.False(t, remembered, "the one release seam clears what the loop left pending")
	})

	// Controls: the three settlements this change must not move.

	t.Run("control: a write that lands is acknowledged once", func(t *testing.T) {
		stage := startDeferredWriteStage(t, client, "57c2e8a1-0f3d-4a96-b7e5-9d1c4b6a8f02")
		const prompt = "a turn written on the first try"
		msg, delivered := deliverTask(t, stage.component, stage.turn("task-deferred-ok", prompt))
		require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision())
		require.Equal(t, int32(1), msg.acks.Load())
		require.Equal(t, prompt, loopRecordOf(t, stage.component, stage.loopID).entity.PendingContinuationPrompt)
		require.NotContains(t, stage.logs.String(), "Deferred turn's record write failed")
	})

	t.Run("control: a lost compare-and-swap releases the loop and retries", func(t *testing.T) {
		stage := startDeferredWriteStage(t, client, "a9d0f6e3-2b18-4e7c-8a45-6f3b1c7d9e50")
		// A foreign writer moves the record past the revision this process holds.
		current, err := stage.bucket.KeyValue.Get(t.Context(), stage.loopID)
		require.NoError(t, err)
		_, err = stage.bucket.KeyValue.Put(t.Context(), stage.loopID, current.Value())
		require.NoError(t, err)

		_, delivered := deliverTask(t, stage.component, stage.turn("task-deferred-cas", "a turn that lost the race"))
		require.Equal(t, natsclient.DeliveryDecisionRetry, delivered.Decision())
		require.Contains(t, stage.logs.String(), "Deferred continuation lost the record race")
		_, err = stage.handler.GetLoop(stage.loopID)
		require.Error(t, err, "a lost compare-and-swap releases the loop, as before")
	})

	t.Run("control: a size refusal is acknowledged, not retried", func(t *testing.T) {
		stage := startDeferredWriteStage(t, client, "1f8b3c9d-7e20-4d61-a3f7-0b5e2c8a6d94")
		stage.bucket.failWith(nats.ErrMaxPayload)
		msg, delivered := deliverTask(t, stage.component, stage.turn("task-deferred-ceiling", "a turn the ceiling refuses"))
		require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision(),
			"a size refusal is deterministic; retrying it parks the lane to max_deliver for nothing")
		require.Equal(t, int32(1), msg.acks.Load())
		require.True(t, strings.Contains(stage.logs.String(), "exceeds the NATS payload ceiling"))
		_, remembered := stage.component.pendingTaskResult("task-deferred-ceiling", stage.loopID)
		require.False(t, remembered)
	})
}
