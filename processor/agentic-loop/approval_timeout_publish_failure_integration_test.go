//go:build integration

package agenticloop

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAnApprovalTimeoutWhosePublicationFailedLeavesTheRecordAsItWas is I1 on
// the one publisher that is not a carrier call site (#1330, owner round 4
// finding 1).
//
// The approval-timeout auto-reject MINTS: the rejection runs through
// handleRejectedApproval -> HandleToolResult -> handleToolsComplete, which
// increments the iteration and mints the loop's next request. The sweeper
// publishes that request and then writes the record itself
// (approval_sweeper.go), so when the publication fails and KV stays writable
// the two halves disagree in the direction I1 forbids: the record names R2
// while the stream retains only R1. Every later cold read of that loop then
// meets adoptNewerRetainedRequest's I1 arm — "the record names a request the
// stream does not retain" — which is Fatal, so a routine redelivery
// quarantines a lane instead of recovering a loop that merely had an approval
// time out. The gate is already resolved in memory, so no later sweep retries
// the publication.
//
// The two halves are told apart only by a real broker: the record's fields and
// the per-subject message count are both durable facts here, and the
// replacement below reads them through the production reader rather than
// through a double. The component's client cannot reach the server — the same
// arrangement birthWhosePublishNeverLanded uses — while the KV handle
// initializeKVBuckets acquired stays bound to the live connection, so the
// publication fails on the production path with the record still writable.
//
// What happens to the loop this process advanced IN MEMORY is #1362's: the
// sweeper is a timer, so it has no delivery to classify and no retry policy
// yet (design § 5.6). This test asserts the DURABLE half, which is the half a
// replacement recovers from.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAnApprovalTimeoutWhosePublicationFailedLeavesTheRecordAsItWas(t *testing.T) {
	client := newLoopNATS(t)
	c, handler := startLoopProcess(t, client, DefaultConfig())
	loopID := gatedLoop(t, client, c, handler, "task-approval-timeout-publish-failure")
	requestSubject := "agent.request." + loopID

	gated := loopRecordOf(t, c, loopID)
	require.Equal(t, agentic.LoopStateAwaitingApproval, gated.entity.State)
	require.NotNil(t, gated.entity.PendingApproval,
		"the fixture must really gate the loop, or the sweep below has no candidate")
	firstRequest := gated.entity.PublishedRequestID
	require.Equal(t, uint64(1), messagesOn(t, client, requestSubject),
		"the gate mints no request, so R1 is all the stream holds for this loop")
	require.Equal(t, firstRequest, retainedRequestIdentity(t, client, requestSubject))

	// The deadline elapses. Backdated rather than waited on: the configured
	// wait is 12h and the sweep reads RequestedAt + Timeout.
	entity, err := handler.GetLoop(loopID)
	require.NoError(t, err)
	entity.PendingApproval.RequestedAt = time.Now().UTC().Add(-2 * DefaultConfig().ApprovalTimeout())
	require.NoError(t, handler.UpdateLoop(entity))
	require.Len(t, handler.loopManager.SnapshotExpiredApprovals(time.Now().UTC()), 1,
		"the sweep must have exactly this loop to work on")

	// The publication fails; the record does not.
	var logs strings.Builder
	c.logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	c.natsClient = unpublishableClient(t)

	c.sweepExpiredApprovals(t.Context())

	require.NotEqual(t, firstRequest, handler.loopManager.OutstandingRequest(loopID),
		"fixture check: the auto-reject must have minted the loop's next request, or the record "+
			"below was never at risk of naming one the stream does not retain")
	require.Equal(t, uint64(1), messagesOn(t, client, requestSubject),
		"fixture check: the minted request must NOT have reached the stream")

	// assert, not require: the name, the iteration, the revision and the gate
	// are four readings of the same advance, and a run that stops at the first
	// reports a quarter of what an operator would have to recover from.
	after := loopRecordOf(t, c, loopID)
	assert.Equal(t, firstRequest, after.entity.PublishedRequestID,
		"the record names a request the stream does not retain: every cold read of this loop now "+
			"meets adoptNewerRetainedRequest's Fatal I1 arm")
	assert.Equal(t, gated.entity.Iterations, after.entity.Iterations,
		"iterations moved in an update whose published_request_id had no published request behind it")
	assert.Equal(t, gated.revision, after.revision,
		"a sweep whose publication failed must leave the record exactly as it read it")
	assert.Equal(t, agentic.LoopStateAwaitingApproval, after.entity.State,
		"the gate is the loop's recoverable state; resolving it durably spends the human's answer")
	assert.Contains(t, logLineContaining(t, logs.String(), "approval timeout auto-reject did not publish its results"),
		"loop_id="+loopID,
		"the skip is a declared event and must name the loop it left gated")

	gate := after.entity.PendingApproval
	require.NotNil(t, gate, "without the gate the record cannot be recovered from at all")

	// A replacement takes the gating tool result again — a lost acknowledgement
	// is ordinary at-least-once delivery — rebuilt from the record's own gate,
	// which is the only place a replacement could find it. It must be
	// acknowledged as work the record already applied; against a record naming
	// an unretained R2 it is quarantined by step 0 instead.
	replacement, _ := startLoopProcess(t, client, DefaultConfig())
	_, redelivered := deliverToolResult(t, replacement, agentic.ToolResult{
		CallID:      gate.CallID,
		Name:        gate.ToolName,
		LoopID:      loopID,
		Error:       gate.Reason,
		RequestID:   gate.RequestID,
		ExecutionID: gate.ExecutionID,
		CallOrdinal: gate.CallOrdinal,
	})
	require.Equal(t, natsclient.DeliveryDecisionAck, redelivered.Decision(),
		"a record naming a request nothing retains quarantines the tool lane on a routine redelivery")
}
