package agenticdispatch

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The two loops of the drift hazard: A is this channel's, B is the same user's
// live loop elsewhere, which is where the tracker's fallback lands once A is
// gone.
const (
	ambiguousLoopA = "1b4e28ba-2fa1-11d2-883f-0016d3cca427"
	ambiguousLoopB = "d9428888-122b-11e1-b85c-61cd3cbb3210"
)

// cancelAmbiguityComponent builds the command lane over a publisher seam that
// behaves like a broker whose acknowledgement was lost: `stores` decides
// whether the signal reached the world before `publishErr` was returned, which
// is exactly the fact the error itself cannot carry. Returns the slice the
// seam appends to, so a test can ask what the world actually holds.
func cancelAmbiguityComponent(t *testing.T, stores bool, publishErr error) (*Component, *[]agentic.UserSignal) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := &Component{
		config:        DefaultConfig(),
		decoder:       message.NewDecoder(payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)),
		logger:        logger,
		loopTracker:   NewLoopTrackerWithLogger(logger),
		metrics:       getMetrics(metric.NewMetricsRegistry()),
		modelRegistry: newTestRegistry(),
		registry:      NewCommandRegistry(),
	}
	c.config.Permissions.CancelOwn = true
	require.True(t, c.config.AutoContinue, "the bare form needs the default auto_continue")
	c.registerBuiltinCommands()

	stored := &[]agentic.UserSignal{}
	c.publishSignalFn = func(_ context.Context, _ string, data []byte) error {
		if stores {
			var envelope struct {
				Payload agentic.UserSignal `json:"payload"`
			}
			require.NoError(t, json.Unmarshal(data, &envelope))
			*stored = append(*stored, envelope.Payload)
		}
		return publishErr
	}

	track := func(loopID, channelID string) {
		c.loopTracker.Track(&LoopInfo{
			LoopID: loopID, TaskID: "task-" + loopID, UserID: "operator-1",
			ChannelType: "http", ChannelID: channelID, State: "executing", CreatedAt: time.Now(),
		})
	}
	track(ambiguousLoopA, "session-a")
	track(ambiguousLoopB, "session-b")
	withPersistedLoops(c, map[string]*agentic.LoopEntity{
		ambiguousLoopA: {
			ID: ambiguousLoopA, UserID: "operator-1", ChannelType: "http", ChannelID: "session-a",
			State: agentic.LoopStateExecuting, MaxIterations: 5,
		},
		ambiguousLoopB: {
			ID: ambiguousLoopB, UserID: "operator-1", ChannelType: "http", ChannelID: "session-b",
			State: agentic.LoopStateExecuting, MaxIterations: 5,
		},
	})
	return c, stored
}

// cancelCommandMessage encodes a /cancel delivery arriving on session-a.
func cancelCommandMessage(t *testing.T, content string) []byte {
	t.Helper()
	data, err := json.Marshal(message.NewBaseMessage(
		(&agentic.UserMessage{}).Schema(),
		&agentic.UserMessage{
			MessageID: "msg-cancel-ambiguous", ChannelType: "http", ChannelID: "session-a",
			UserID: "operator-1", Content: content,
		}, "test"))
	require.NoError(t, err)
	return data
}

// A failed publish reports what the CLIENT saw. For most failures that says
// nothing about the server's store: a PubAck lost on the way back is
// indistinguishable from a signal that never arrived. Retrying on that
// uncertainty replays a bare /cancel whose target this component picks afresh,
// and if the first attempt did store, A is already cancelling — so the
// redelivery falls through A to the user's next live loop and cancels B, which
// the message never named. That is the published-then-unanswered hazard
// reached through an error instead of through a response.
//
// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestBareCancelWithUnconfirmedSignalQuarantines(t *testing.T) {
	t.Run("an ambiguous failure on a resolved target quarantines", func(t *testing.T) {
		// ErrNoStreamResponse is the ambiguity in its purest form: no responder
		// answered, which cannot tell a stream that never received the message
		// from one whose reply was lost.
		c, stored := cancelAmbiguityComponent(t, true, jetstream.ErrNoStreamResponse)

		// assert, not require: a wrong decision must not skip the consequence
		// assertion at the bottom, which is what says the misclassification costs
		// a live loop.
		decision, err := c.handleUserMessage(t.Context(), cancelCommandMessage(t, "/cancel"))
		assert.Equal(t, natsclient.DeliveryDecisionQuarantine, decision,
			"the delivery cannot prove its signal was refused, and the message does not name the loop it may have cancelled")
		require.Error(t, err)
		require.ErrorContains(t, err, "publish signal")
		require.Equal(t, []string{ambiguousLoopA}, signalledLoopIDs(*stored),
			"the attempt went to this channel's loop")

		// Production redelivers a Retry and never redelivers a Quarantine, so the
		// cost of the wrong answer is only visible by driving it.
		if decision == natsclient.DeliveryDecisionRetry {
			c.loopTracker.UpdateState(ambiguousLoopA, "cancelled")
			withPersistedLoops(c, map[string]*agentic.LoopEntity{
				ambiguousLoopA: {
					ID: ambiguousLoopA, UserID: "operator-1", ChannelType: "http", ChannelID: "session-a",
					State: agentic.LoopStateCancelled, MaxIterations: 5,
				},
				ambiguousLoopB: {
					ID: ambiguousLoopB, UserID: "operator-1", ChannelType: "http", ChannelID: "session-b",
					State: agentic.LoopStateExecuting, MaxIterations: 5,
				},
			})
			_, err = c.handleUserMessage(t.Context(), cancelCommandMessage(t, "/cancel"))
			require.Error(t, err)
		}

		require.NotContains(t, signalledLoopIDs(*stored), ambiguousLoopB,
			"a redelivery re-resolved the target and cancelled a loop the user never named")
	})

	t.Run("the same failure on a named target retries", func(t *testing.T) {
		c, stored := cancelAmbiguityComponent(t, true, jetstream.ErrNoStreamResponse)

		decision, err := c.handleUserMessage(t.Context(), cancelCommandMessage(t, "/cancel "+ambiguousLoopA))

		require.Equal(t, natsclient.DeliveryDecisionRetry, decision,
			"the message names the loop, so the redelivery re-reads THAT loop and cannot drift onto another")
		require.Error(t, err)
		require.Equal(t, []string{ambiguousLoopA}, signalledLoopIDs(*stored))
	})

	t.Run("a proven refusal on a resolved target retries", func(t *testing.T) {
		// ErrNotConnected is returned by the client before js.PublishMsg is
		// reached (natsclient/client.go:975-978), so nothing was stored and the
		// redelivery is the thing that gets the user their answer.
		c, stored := cancelAmbiguityComponent(t, false, natsclient.ErrNotConnected)

		decision, err := c.handleUserMessage(t.Context(), cancelCommandMessage(t, "/cancel"))

		require.Equal(t, natsclient.DeliveryDecisionRetry, decision,
			"a refusal the client proves leaves the world unchanged; quarantining it would latch the lane on a disconnect")
		require.Error(t, err)
		require.Empty(t, *stored, "the fixture must not claim a store the refusal rules out")
	})
}

func signalledLoopIDs(signals []agentic.UserSignal) []string {
	ids := []string{}
	for _, signal := range signals {
		ids = append(ids, signal.LoopID)
	}
	return ids
}
