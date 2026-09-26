package agenticloop

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/deliverylane"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// payloadCeilingBucket refuses a write whose value exceeds its ceiling with the
// error the NATS client returns before it sends a message larger than the
// server's max_payload (nats.ErrMaxPayload). Every other behaviour — revisions,
// create-once, compare-and-swap — is recordingLoopBucket's.
type payloadCeilingBucket struct {
	*recordingLoopBucket
	ceiling int
}

func (b *payloadCeilingBucket) Create(
	ctx context.Context, key string, value []byte, opts ...jetstream.KVCreateOpt,
) (uint64, error) {
	if len(value) > b.ceiling {
		return 0, nats.ErrMaxPayload
	}
	return b.recordingLoopBucket.Create(ctx, key, value, opts...)
}

func (b *payloadCeilingBucket) Update(ctx context.Context, key string, value []byte, revision uint64) (uint64, error) {
	if len(value) > b.ceiling {
		return 0, nats.ErrMaxPayload
	}
	return b.recordingLoopBucket.Update(ctx, key, value, revision)
}

// ceilingLoop births a loop through the production handler — so its first
// request is outstanding and a continuation defers behind it — and gives it a
// record under a bucket whose ceiling sits headroom bytes above that record.
func ceilingLoop(t *testing.T, headroom int) (*Component, *MessageHandler, *bytes.Buffer, string) {
	t.Helper()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	handler := NewMessageHandler(DefaultConfig())
	birth, err := handler.HandleTask(t.Context(), agentic.TaskMessage{
		TaskID: "task-ceiling", Role: "general", Model: "model-a", Prompt: "a short first turn",
	})
	require.NoError(t, err)
	c := releaseTestComponent(t, handler)
	c.logger = logger
	entity, err := handler.GetLoop(birth.LoopID)
	require.NoError(t, err)
	rendered, err := json.Marshal(entity)
	require.NoError(t, err)
	c.loopsBucket = &payloadCeilingBucket{recordingLoopBucket: &recordingLoopBucket{}, ceiling: len(rendered) + headroom}
	seedLoopRecord(t, c, birth.LoopID)
	return c, handler, &logs, birth.LoopID
}

// TestALoopRecordThePayloadCeilingRefusesIsNotRetried is the record's size
// bound (#1365, #857; design OQ2 (a)): the WHOLE rendered record shares the
// server's max_payload, and the client's refusal is observed at each of the
// three writes that can meet it, never predicted.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestALoopRecordThePayloadCeilingRefusesIsNotRetried(t *testing.T) {
	t.Run("a birth record the ceiling refuses terminates the task and releases the loop", func(t *testing.T) {
		const loopID = "3f6a8c2e-1d4b-4a7e-9c05-6b7a8d9e0f12"
		c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
		// Below any record: the refusal is what is under test, not what
		// made the record large.
		c.loopsBucket = &payloadCeilingBucket{recordingLoopBucket: &recordingLoopBucket{}, ceiling: 16}
		c.metrics = getMetrics(metric.NewMetricsRegistry())
		rejected := func() float64 {
			return testutil.ToFloat64(c.metrics.taskIntakeRejections.WithLabelValues(
				taskIntakeBirthLane, taskIntakeRecordExceedsCeilingReason))
		}
		before := rejected()

		msg, settled := deliverBirth(t, c, loopID)

		// Before: WrapTransient → Retry, the same bytes into the same refusal
		// for the redelivery budget on a lane at MaxAckPending 1.
		require.Equal(t, natsclient.DeliveryDecisionTerminate, settled.Decision())
		require.Equal(t, int32(1), msg.terms.Load())
		require.ErrorIs(t, settled.Cause(), nats.ErrMaxPayload)
		require.Contains(t, settled.Cause().Error(), loopID, "the cause names the loop")
		require.Contains(t, settled.Cause().Error(), "bytes", "the cause names the record's size")
		_, err := c.handler.GetLoop(loopID)
		require.Error(t, err, "a terminated birth must not leave its loop in memory")
		// The refusal is declared, not only logged: counted as an intake
		// rejection (the graph stamp is the integration test's half).
		assert.Equal(t, before+1, rejected(), "the terminated birth is not counted")
	})

	t.Run("a marker write the ceiling refuses drops the text, acknowledges, and the next carrier write fits", func(t *testing.T) {
		c, handler, logs, loopID := ceilingLoop(t, 1024)
		bigTurn := strings.Repeat("a turn larger than the record's headroom ", 64)

		msg := &loopDeliveryOwnerMsg{data: baseMessageBytes(t, &agentic.TaskMessage{
			TaskID: "task-ceiling-2", LoopID: loopID, Role: "general", Model: "model-a", Prompt: bigTurn,
		})}
		settled, admitted := deliverylane.Consume(t.Context(), msg,
			heartbeatPolicyForTest(t, "agent.task", c.taskInputHandler(time.Minute)),
			deliverylane.NewAdmission(nil, nil))
		require.True(t, admitted)

		require.Equal(t, natsclient.DeliveryDecisionAck, settled.Decision(),
			"a refused marker write is best-effort, as any other marker write failure")
		entity, err := handler.GetLoop(loopID)
		require.NoError(t, err)
		require.True(t, entity.PendingContinuation, "the turn is still deferred in the live loop")
		// assert, not require: the text left on the entity and the refusal it
		// causes one write later are the same defect seen twice, and the
		// second is the one that costs the loop its record.
		assert.Empty(t, entity.PendingContinuationPrompt,
			"the refused text stayed on the entity, so every later record write renders it into the same refusal")
		warned := logLineContaining(t, logs.String(), "exceeds the NATS payload ceiling")
		require.Contains(t, warned, "loop_id="+loopID)
		require.Contains(t, warned, "record_bytes=")

		require.NoError(t, c.persistLoopState(t.Context(), loopID),
			"the loop's next record write was refused for the text the marker write could not land")
	})

	t.Run("a refused marker write leaves a later turn's text alone", func(t *testing.T) {
		c, handler, _, loopID := ceilingLoop(t, 1024)
		refused := strings.Repeat("a turn larger than the record's headroom ", 64)
		const later = "a later, smaller turn"
		_, deferred, err := handler.loopManager.attachContinuation(loopID, "task-ceiling-2", refused)
		require.NoError(t, err)
		require.True(t, deferred)
		// The second turn is admitted before the first turn's marker write
		// runs: the entity now holds the LATER turn, which the refusal of the
		// earlier one does not own.
		_, _, err = handler.loopManager.attachContinuation(loopID, "task-ceiling-3", later)
		require.NoError(t, err)

		require.ErrorIs(t, c.persistDeferredContinuationMarker(t.Context(), loopID, refused), nats.ErrMaxPayload)

		entity, err := handler.GetLoop(loopID)
		require.NoError(t, err)
		require.Equal(t, later, entity.PendingContinuationPrompt,
			"the refusal of one turn cleared another turn's text")
	})

	t.Run("a carrier write the ceiling refuses quarantines, as any carrier write failure", func(t *testing.T) {
		c, handler, _, loopID := ceilingLoop(t, 1024)
		bigTurn := strings.Repeat("a turn larger than the record's headroom ", 64)
		_, deferred, err := handler.loopManager.attachContinuation(loopID, "task-ceiling-2", bigTurn)
		require.NoError(t, err)
		require.True(t, deferred)

		// A non-terminal result with nothing to publish: the carrier goes
		// straight to its compare-and-swap, which renders the in-memory entity
		// with the turn's text on it.
		result := HandlerResult{LoopID: loopID, State: agentic.LoopStateExploring,
			PublishedMessages: []PublishedMessage{}}
		settled, admitted := deliverylane.Consume(t.Context(), &loopDeliveryOwnerMsg{data: []byte("{}")},
			heartbeatPolicyForTest(t, "agent.response", func(ctx context.Context, _ []byte) error {
				return c.persistHandlerResult(ctx, result)
			}), deliverylane.NewAdmission(nil, nil))
		require.True(t, admitted)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, settled.Decision())
		require.ErrorIs(t, settled.Cause(), nats.ErrMaxPayload)
	})
}
