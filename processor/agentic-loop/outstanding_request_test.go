package agenticloop

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The outstanding mark is what tells a superseded response from a live one, so
// what clears it decides whether a stale answer can un-mark a request that is
// still in flight. SettleRequest matches on the request id for exactly that
// reason, and nothing exercised the mismatch: an unconditional delete passes
// every other test in this package.
//
// The sequence is the one production produces when a request is superseded —
// two requests minted for one loop, the older one answered last.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestSettleRequestOnlyClearsTheRequestItNames(t *testing.T) {
	m := NewLoopManager()
	loopID, err := m.CreateLoop("task-settle", "general", "model", 5)
	require.NoError(t, err)

	first := m.GenerateRequestID(loopID)
	m.TrackRequest(first, loopID)
	require.NoError(t, m.IncrementIteration(loopID))
	second := m.GenerateRequestID(loopID)
	m.TrackRequest(second, loopID)
	require.NotEqual(t, first, second, "the fixture must mint two distinct requests")

	m.SettleRequest(loopID, first)

	require.Equal(t, second, m.OutstandingRequest(loopID),
		"a late answer to a superseded request cleared the mark of the request still in flight")

	m.SettleRequest(loopID, second)
	require.Empty(t, m.OutstandingRequest(loopID),
		"the request the loop is actually waiting on must still settle")
}

// The same matching rule on the carrier of a deferred turn. A response for some
// other request must not end a deferral the carrying request has not answered
// yet — that would settle the loop with the user's turn unanswered.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestSettleRequestEndsTheDeferralOnlyForTheCarrier(t *testing.T) {
	m := NewLoopManager()
	loopID, err := m.CreateLoop("task-carrier", "general", "model", 5)
	require.NoError(t, err)

	first := m.GenerateRequestID(loopID)
	m.TrackRequest(first, loopID)
	_, deferred, err := m.attachContinuation(loopID, "task-carrier-2")
	require.NoError(t, err)
	require.True(t, deferred)

	require.NoError(t, m.IncrementIteration(loopID))
	carrier := m.GenerateRequestID(loopID)
	m.TrackRequest(carrier, loopID)

	// The superseded first request answers late. It carried nothing.
	m.SettleRequest(loopID, first)
	entity, err := m.GetLoop(loopID)
	require.NoError(t, err)
	require.True(t, entity.PendingContinuation,
		"a response for a request that carries nothing ended the deferral")
	require.Equal(t, carrier, entity.PendingContinuationRequestID)

	m.SettleRequest(loopID, carrier)
	entity, err = m.GetLoop(loopID)
	require.NoError(t, err)
	require.False(t, entity.PendingContinuation,
		"the carrier's own answer proves the turn was sent and must end the deferral")
	require.Empty(t, entity.PendingContinuationRequestID)
}

// The recovery lookup repairs ROUTING. Before the split it called TrackRequest,
// so reading "which loop does this request belong to" also announced that the
// loop was waiting on that request — and the caller is the response path, where
// the request has just been answered. A loop that had already moved on would
// have been re-marked as waiting on the answered request, which is the state
// the superseded-response guard reads.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestRequestRecoveryRepairsRoutingWithoutMarkingOutstanding(t *testing.T) {
	m := NewLoopManager()
	loopID, err := m.CreateLoop("task-recovery", "general", "model", 5)
	require.NoError(t, err)

	answered := m.GenerateRequestID(loopID)
	require.NoError(t, m.IncrementIteration(loopID))
	live := m.GenerateRequestID(loopID)
	m.TrackRequest(live, loopID)

	// `answered` was never tracked in this process — the cache-miss case the
	// structured id exists to survive.
	recovered, ok := m.GetLoopForRequestWithRecovery(answered)
	require.True(t, ok)
	require.Equal(t, loopID, recovered)
	routed, ok := m.GetLoopForRequest(answered)
	require.True(t, ok, "the routing map must have been repaired")
	require.Equal(t, loopID, routed)

	require.Equal(t, live, m.OutstandingRequest(loopID),
		"a lookup re-marked the loop as waiting on a request it had already moved past")
}
