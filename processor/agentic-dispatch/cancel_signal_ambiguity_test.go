package agenticdispatch

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The two loops of the drift hazard, both on the SAME user/channel route: A is
// the loop this delivery resolves, B is the one born on that route while A was
// settling, which is what a redelivery would resolve instead. Under #1329 the
// resolution reads durable loop authority through activeLoop, which matches the
// exact route and refuses ambiguity — so the cross-channel fall-through is gone
// and same-route rebirth is the whole of what survives.
const (
	ambiguousLoopA = "1b4e28ba-2fa1-11d2-883f-0016d3cca427"
	ambiguousLoopB = "d9428888-122b-11e1-b85c-61cd3cbb3210"
)

// ambiguityLoop is one record of the session-a route in a given state, written
// to both the shared projection and the exact record so the two agree.
func ambiguityLoop(loopID string, state agentic.LoopState) *agentic.LoopEntity {
	return &agentic.LoopEntity{
		ID: loopID, TaskID: "task-" + loopID, UserID: "operator-1",
		ChannelType: "http", ChannelID: "session-a",
		State: state, MaxIterations: 5,
	}
}

// cancelAmbiguityWorld builds the command lane over a publisher seam that
// behaves like a broker whose acknowledgement was lost: `stores` decides
// whether the signal reached the world before `publishErr` was returned, which
// is exactly the fact the error itself cannot carry. `current` is the world
// this delivery reads — the shared projection and the exact record both. The
// signals it puts on the wire are appended to `stored`, which the caller owns,
// so a redelivery against a CHANGED world can be driven through a second lane
// and still be asked what the world as a whole now holds.
func cancelAmbiguityWorld(
	t *testing.T, stores bool, publishErr error, stored *[]agentic.UserSignal, current ...*agentic.LoopEntity,
) *Component {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := &Component{
		config:        DefaultConfig(),
		decoder:       message.NewDecoder(payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)),
		logger:        logger,
		metrics:       getMetrics(metric.NewMetricsRegistry()),
		modelRegistry: newTestRegistry(),
		registry:      NewCommandRegistry(),
	}
	c.config.Permissions.CancelOwn = true
	require.True(t, c.config.AutoContinue, "the bare form needs the default auto_continue")
	c.registerBuiltinCommands()

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

	seedCurrentLoops(t, c, current...)
	records := map[string]*agentic.LoopEntity{}
	for _, record := range current {
		records[record.ID] = record
	}
	withPersistedLoops(c, records)
	return c
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
// and if the first attempt did store, A settles — so the redelivery resolves
// the loop born on the same route in between and cancels B, which the message
// never named. That is the published-then-unanswered hazard reached through an
// error instead of through a response.
//
// The teeth are the classification and the attempt it is read from: the
// Quarantine assertion and "the attempt went to this route's loop". The
// NotContains at the end of the first subtest is INERT under the asserted
// classification, because production never redelivers a quarantined delivery
// and the block that would cancel B is therefore conditional on the wrong
// answer; it is the demonstration of what a Retry would cost, not the
// discriminator. Mutation evidence: deleting the noteSignalAttempt call flips
// the Quarantine assertion and then the NotContains fires; dropping the
// proven-refusal conjunct flips the third subtest.
//
// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestBareCancelWithUnconfirmedSignalQuarantines(t *testing.T) {
	t.Run("an ambiguous failure on a resolved target quarantines", func(t *testing.T) {
		// ErrNoStreamResponse is the ambiguity in its purest form: no responder
		// answered, which cannot tell a stream that never received the message
		// from one whose reply was lost.
		stored := &[]agentic.UserSignal{}
		c := cancelAmbiguityWorld(t, true, jetstream.ErrNoStreamResponse, stored,
			ambiguityLoop(ambiguousLoopA, agentic.LoopStateExecuting))

		// assert, not require: a wrong decision must not skip the consequence
		// assertion at the bottom, which is what says the misclassification costs
		// a live loop.
		decision, err := c.handleUserMessage(t.Context(), cancelCommandMessage(t, "/cancel"))
		assert.Equal(t, natsclient.DeliveryDecisionQuarantine, decision,
			"the delivery cannot prove its signal was refused, and the message does not name the loop it may have cancelled")
		require.Error(t, err)
		require.ErrorContains(t, err, "publish signal")
		require.Equal(t, []string{ambiguousLoopA}, signalledLoopIDs(*stored),
			"the attempt went to this route's loop")

		// Production redelivers a Retry and never redelivers a Quarantine, so the
		// cost of the wrong answer is only visible by driving it — against the
		// world the first attempt left behind: A settled by the signal that did
		// store, and B started on the same route in between.
		if decision == natsclient.DeliveryDecisionRetry {
			redelivery := cancelAmbiguityWorld(t, true, jetstream.ErrNoStreamResponse, stored,
				ambiguityLoop(ambiguousLoopA, agentic.LoopStateCancelled),
				ambiguityLoop(ambiguousLoopB, agentic.LoopStateExecuting))
			_, err = redelivery.handleUserMessage(t.Context(), cancelCommandMessage(t, "/cancel"))
			require.Error(t, err)
		}

		require.NotContains(t, signalledLoopIDs(*stored), ambiguousLoopB,
			"a redelivery re-resolved the target and cancelled a loop the user never named")
	})

	t.Run("the same failure on a named target retries", func(t *testing.T) {
		stored := &[]agentic.UserSignal{}
		c := cancelAmbiguityWorld(t, true, jetstream.ErrNoStreamResponse, stored,
			ambiguityLoop(ambiguousLoopA, agentic.LoopStateExecuting))

		decision, err := c.handleUserMessage(t.Context(), cancelCommandMessage(t, "/cancel "+ambiguousLoopA))

		require.Equal(t, natsclient.DeliveryDecisionRetry, decision,
			"the message names the loop, so the redelivery re-reads THAT loop and cannot drift onto another")
		require.Error(t, err)
		require.Equal(t, []string{ambiguousLoopA}, signalledLoopIDs(*stored))
	})

	t.Run("a connection closed mid-publish is ambiguous, not a refusal", func(t *testing.T) {
		// nats.ErrConnectionClosed reads like a refusal and is one at
		// nats.go:4450 — but the sync publish path reaches
		// RequestMsgWithContext, which returns the SAME sentinel at
		// context.go:70 when clearPendingRequestCalls (nats.go:5925-5932)
		// closes the reply channel on a close or ForceReconnect, AFTER
		// createNewRequestAndSend already wrote. errors.Is cannot tell the two
		// sites apart, so the fixture stores: this is the connection that
		// dropped with the signal already gone.
		c, stored := cancelAmbiguityComponent(t, true, nats.ErrConnectionClosed)

		decision, err := c.handleUserMessage(t.Context(), cancelCommandMessage(t, "/cancel"))

		assert.Equal(t, natsclient.DeliveryDecisionQuarantine, decision,
			"a sentinel a post-write site also returns cannot prove the broker stored nothing")
		require.Error(t, err)
		require.Equal(t, []string{ambiguousLoopA}, signalledLoopIDs(*stored),
			"the fixture must have stored, or the test proves nothing about the ambiguous case")

		// The cost of Retry here, driven the same way as the first subtest.
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

	t.Run("a proven refusal on a resolved target retries", func(t *testing.T) {
		// ErrNotConnected is returned by the client before js.PublishMsg is
		// reached (natsclient/client.go:976-978), so nothing was stored and the
		// redelivery is the thing that gets the user their answer.
		stored := &[]agentic.UserSignal{}
		c := cancelAmbiguityWorld(t, false, natsclient.ErrNotConnected, stored,
			ambiguityLoop(ambiguousLoopA, agentic.LoopStateExecuting))

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
