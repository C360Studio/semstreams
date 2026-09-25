package agenticloop

import (
	"context"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/deliverylane"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/require"
)

// The publish-phase half of the commit-unknown rule, without a broker.
//
// A non-gated result publishes first (#1376: the result's shape decides the
// order), and its publish phase is commit-unknown because it emits results one
// at a time: a failure at result k leaves 1..k-1 already PubAck'd with no
// record of how far it got. An unconnected client fails the very first
// publish, which is the same phase and the same wrap: what this observes is
// that a publish-phase error leaves persistHandlerResult fatal-classified with
// the publish's own cause, and that the same result with nothing to publish
// succeeds, so the classification is the publish's and not the path's.
//
// The end-to-end partial case (first publish durable, second fails) needs a
// real stream and lives in TestIntegrationPartialPublishQuarantinesRatherThanRetrying.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestPublishPhaseFailureLeavesPersistHandlerResultFatalClassified(t *testing.T) {
	t.Parallel()

	c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
	// Constructed, never connected: publishToStream refuses with
	// ErrNotConnected before it touches a socket.
	client, err := natsclient.NewClient("nats://127.0.0.1:1")
	require.NoError(t, err)
	c.natsClient = client

	// Non-terminal, and loopsBucket stays nil, so the only thing that can fail
	// in this result is the publish.
	result := HandlerResult{
		LoopID:            "loop-publish-phase",
		State:             agentic.LoopStateExploring,
		PublishedMessages: []PublishedMessage{{Subject: "agent.first", Data: []byte(`{"n":1}`)}},
	}

	persistErr := c.persistHandlerResult(t.Context(), result)
	require.Error(t, persistErr)
	require.True(t, errs.IsFatal(persistErr),
		"a publish-phase failure is commit-unknown and must be fatal-classified, not an ordinary error")
	require.Contains(t, persistErr.Error(), "unknown durability")

	// A result with nothing to publish reaches the same line and succeeds, so
	// the classification above is the publish's, not the path's.
	require.NoError(t, c.persistHandlerResult(t.Context(),
		HandlerResult{LoopID: "loop-publish-phase", State: agentic.LoopStateExploring}))
}

// And the mapping that consumes it. The heartbeat work function must test
// fatal FIRST: a commit-unknown error can never fall through to Retry, and it
// must not be read as Terminate either, because Terminate throws the delivery
// away while an effect may already have happened.
//
// The ordering is observable because the fatal cause here is ALSO a permanent
// delivery error. If the errs.IsFatal arm moved below the PermanentDeliveryError
// arm — or were deleted — this settles Terminate instead of Quarantine.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestHeartbeatWorkMapsFatalToQuarantineAheadOfPermanentAndRetry(t *testing.T) {
	t.Parallel()

	for name, lane := range map[string]struct {
		handlerErr error
		decision   natsclient.DeliveryDecision
		ownerStop  bool
	}{
		"commit-unknown is quarantined, not terminated": {
			handlerErr: errs.WrapFatal(
				natsclient.TerminateDelivery(errors.New("publish result agent.second: not connected")),
				"agentic-loop", "persistHandlerResult", "published results have unknown durability"),
			decision:  natsclient.DeliveryDecisionQuarantine,
			ownerStop: true,
		},
		"a permanent error that is not fatal still terminates": {
			handlerErr: natsclient.TerminateDelivery(errors.New("payload will never decode")),
			decision:   natsclient.DeliveryDecisionTerminate,
		},
		"an ordinary error still retries": {
			handlerErr: errors.New("kv unavailable"),
			decision:   natsclient.DeliveryDecisionRetry,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			msg := &loopDeliveryOwnerMsg{data: []byte("{}")}
			policy := heartbeatPolicyForTest(t, "agent.response",
				func(context.Context, []byte) error { return lane.handlerErr })
			result, admitted := deliverylane.Consume(
				t.Context(), msg, policy, deliverylane.NewAdmission(nil, nil))
			require.True(t, admitted)
			require.Equal(t, lane.decision, result.Decision())
			require.Equal(t, lane.ownerStop, result.OwnerStopRequired())
			if lane.decision == natsclient.DeliveryDecisionQuarantine {
				require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load(),
					"a quarantined delivery attempts no terminal method at all")
			}
		})
	}
}
