package agenticloop

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

// An approval answer does not outlive its loop's deadline (PR #1366
// post-archive review, P1). The LOOP deadline is the record's own TimeoutAt,
// kept across a replacement (owner ruling, #1330 issuecomment-5781101792) —
// not the approval deadline, whose startup re-arm stays deferred (OQ2). Each
// fixture here keeps the approval deadline an hour ahead, so only the loop
// deadline can decide.
//
// Before the fix an approve or a modify dispatched the gated call on an
// expired loop and wrote it executing, and a reject reached HandleToolResult's
// timeout arm, whose populated failure the lane discarded behind a quarantine:
// the record stayed awaiting_approval, no COMPLETE_ marker was created, and
// the approval lane stopped.

var approvalDecisions = []string{
	agentic.ApprovalDecisionApprove,
	agentic.ApprovalDecisionModify,
	agentic.ApprovalDecisionReject,
}

func (a coldApproval) answerOf(decision string) agentic.ApprovalResponse {
	response := a.answer(decision)
	if decision == agentic.ApprovalDecisionModify {
		response.ModifiedArguments = map[string]any{"rule_id": "rule-human-authorized"}
	}
	return response
}

// requireLoopTimedOut asserts the loop's timeout is durable on the record and
// through the terminal owner, and that this process let go of the loop.
func requireLoopTimedOut(t *testing.T, a coldApproval) {
	t.Helper()
	record := persistedLoop(t, a.bucket, coldApprovalLoopID)
	require.Equal(t, agentic.LoopStateFailed, record.State,
		"the original loop deadline must be committed on this delivery")
	require.Equal(t, agentic.OutcomeFailed, record.Outcome)
	require.Contains(t, record.Error, "loop timeout exceeded")
	require.Nil(t, record.PendingApproval, "a durable terminal clears the approval gate")
	require.Contains(t, a.bucket.written(), terminalMarkerKey(coldApprovalLoopID),
		"the timeout must pass through the terminal owner")
	require.False(t, a.held(), "a committed timeout releases the loop")
}

// TestAColdApprovalAnswerForAnExpiredLoopSettlesTheTimeout: the replacement
// gap outran the loop's deadline, so the rebuilt loop fails on the answer and
// the answer is acknowledged — whatever the human decided.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAColdApprovalAnswerForAnExpiredLoopSettlesTheTimeout(t *testing.T) {
	for _, decision := range approvalDecisions {
		t.Run(decision, func(t *testing.T) {
			a := newColdApproval(t, nil, func(e *agentic.LoopEntity) {
				e.TimeoutAt = time.Now().Add(-time.Minute)
			})

			settled, err := a.deliver(t, a.answerOf(decision))

			require.NoError(t, err, "an expired rebuilt loop commits its timeout and acknowledges")
			require.Equal(t, natsclient.DeliveryDecisionAck, settled)
			requireLoopTimedOut(t, a)
		})
	}
}

// TestAWarmApprovalAnswerForAnExpiredLoopSettlesTheTimeout is the same answer
// reaching the process that holds the loop. The warm path had the same
// defect: nothing on it read the loop's deadline.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAWarmApprovalAnswerForAnExpiredLoopSettlesTheTimeout(t *testing.T) {
	for _, decision := range approvalDecisions {
		t.Run(decision, func(t *testing.T) {
			a := newColdApproval(t, nil, nil)
			response := a.answerOf(decision)
			rebuilt, err := a.c.settleApprovalResponseWithoutLoop(t.Context(), response)
			require.NoError(t, err)
			require.True(t, rebuilt, "fixture: this process holds the gated loop")
			held, err := a.c.handler.GetLoop(coldApprovalLoopID)
			require.NoError(t, err)
			require.NoError(t, a.c.handler.loopManager.restoreDeadline(
				coldApprovalLoopID, held.StartedAt, time.Now().Add(-time.Minute)))

			settled, err := a.deliver(t, response)

			require.NoError(t, err, "an expired held loop commits its timeout and acknowledges")
			require.Equal(t, natsclient.DeliveryDecisionAck, settled)
			requireLoopTimedOut(t, a)
		})
	}
}

// TestAnApprovalForAnExpiredLoopDispatchesNothing reads the handler's result
// directly, because the component's publications are unobservable without a
// broker: an approved or modified call must not reach tool.execute once the
// loop's deadline has passed.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAnApprovalForAnExpiredLoopDispatchesNothing(t *testing.T) {
	for _, decision := range approvalDecisions {
		t.Run(decision, func(t *testing.T) {
			a := newColdApproval(t, nil, func(e *agentic.LoopEntity) {
				e.TimeoutAt = time.Now().Add(-time.Minute)
			})
			response := a.answerOf(decision)
			rebuilt, err := a.c.settleApprovalResponseWithoutLoop(t.Context(), response)
			require.NoError(t, err)
			require.True(t, rebuilt)

			result, err := a.c.handler.HandleApprovalResponse(t.Context(), response)

			require.Error(t, err, "the timeout is the loop's business failure")
			require.Equal(t, agentic.LoopStateFailed, result.State)
			require.NotNil(t, result.FailureState, "the populated failure is what the lane commits")
			for _, published := range result.PublishedMessages {
				require.False(t, strings.HasPrefix(published.Subject, "tool.execute"),
					"an expired loop dispatched %s", published.Subject)
			}
			require.Empty(t, a.c.handler.loopManager.GetPendingTools(coldApprovalLoopID),
				"no call is in flight on an expired loop")
		})
	}
}

// TestTheApprovalSweepSettlesAnExpiredLoopOnItsTimeout is the same defect one
// caller over: the approval-timeout sweeper feeds its auto-reject through
// HandleApprovalResponse, and on a loop past its LOOP deadline that returns the
// populated timeout failure with its error. The sweeper logged the error and
// dropped the failure, leaving memory terminal and the record gated.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestTheApprovalSweepSettlesAnExpiredLoopOnItsTimeout(t *testing.T) {
	a := newColdApproval(t, nil, func(e *agentic.LoopEntity) {
		e.TimeoutAt = time.Now().Add(-time.Minute)
		e.PendingApproval.RequestedAt = time.Now().Add(-2 * time.Hour)
	})
	rebuilt, err := a.c.settleApprovalResponseWithoutLoop(t.Context(), a.answer(agentic.ApprovalDecisionReject))
	require.NoError(t, err)
	require.True(t, rebuilt, "fixture: this process holds the gated loop and sweeps its deadline")

	a.c.sweepExpiredApprovals(t.Context())

	requireLoopTimedOut(t, a)
}

// TestAnExpiredApprovalSettlesOnWhatItsTerminalCommitReturns pins the approval
// lane's disposition when the timeout's terminal commit does not land (PR
// #1366 re-review MEDIUM-1). The lane settles on commitTerminal's own
// classification: a lost compare-and-swap is retried, with the loop already
// released so the redelivery re-reads the record; any other failure leaves the
// commit unknown and is quarantined, with the loop released so memory never
// answers for a terminal that is not durable.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAnExpiredApprovalSettlesOnWhatItsTerminalCommitReturns(t *testing.T) {
	t.Run("a lost compare-and-swap on the record is retried", func(t *testing.T) {
		a := newColdApproval(t, nil, func(e *agentic.LoopEntity) {
			e.TimeoutAt = time.Now().Add(-time.Minute)
		})
		response := a.answer(agentic.ApprovalDecisionApprove)
		rebuilt, err := a.c.settleApprovalResponseWithoutLoop(t.Context(), response)
		require.NoError(t, err)
		require.True(t, rebuilt, "fixture: this process holds the expired loop")
		// A foreign writer moves the record past the revision this process read.
		foreign, ok := a.bucket.value(coldApprovalLoopID)
		require.True(t, ok)
		_, err = a.bucket.Put(t.Context(), coldApprovalLoopID, foreign)
		require.NoError(t, err)

		settled, err := a.deliver(t, response)

		require.ErrorIs(t, err, natsclient.ErrKVRevisionMismatch)
		require.Contains(t, a.bucket.written(), terminalMarkerKey(coldApprovalLoopID),
			"fixture: the commit reached the record, its last step")
		require.Equal(t, natsclient.DeliveryDecisionRetry, settled,
			"a lost compare-and-swap wrote nothing to the record; the redelivery re-reads it")
		require.Equal(t, agentic.LoopStateAwaitingApproval, persistedLoop(t, a.bucket, coldApprovalLoopID).State)
		require.False(t, a.held(), "the lost compare-and-swap released the loop")
	})

	t.Run("a failed marker create is quarantined and releases the loop", func(t *testing.T) {
		a := newColdApproval(t, nil, func(e *agentic.LoopEntity) {
			e.TimeoutAt = time.Now().Add(-time.Minute)
		})
		a.bucket.failPrefix = terminalMarkerKey(coldApprovalLoopID)
		a.bucket.arm(errors.New("injected marker write failure"))

		settled, err := a.deliver(t, a.answer(agentic.ApprovalDecisionApprove))

		require.Error(t, err)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, settled,
			"the terminal commit is unknown: never acknowledged, never blindly retried")
		require.NotContains(t, a.bucket.written(), terminalMarkerKey(coldApprovalLoopID))
		require.Equal(t, agentic.LoopStateAwaitingApproval, persistedLoop(t, a.bucket, coldApprovalLoopID).State)
		require.False(t, a.held(), "memory never holds a terminal the owner did not commit")
	})
}

// TestASweepTimeoutWhosePublishFailedSettlesOnTheNextAnswer pins the widened
// residual (PR #1366 re-review MEDIUM-3; a residual beside owner ruling 2,
// #1362 issuecomment-5809906669, which names only max_iterations): the sweep commits the loop's timeout marker, its
// publication fails, and the record stays awaiting_approval because a timer is
// never redelivered. The next answer to that gate rebuilds the loop, re-derives
// the same timeout, and adopts the durable marker — the kinds match — so the
// loop settles on that answer rather than being applied.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestASweepTimeoutWhosePublishFailedSettlesOnTheNextAnswer(t *testing.T) {
	a := newColdApproval(t, nil, func(e *agentic.LoopEntity) {
		e.TimeoutAt = time.Now().Add(-time.Minute)
		e.PendingApproval.RequestedAt = time.Now().Add(-2 * time.Hour)
	})
	rebuilt, err := a.c.settleApprovalResponseWithoutLoop(t.Context(), a.answer(agentic.ApprovalDecisionReject))
	require.NoError(t, err)
	require.True(t, rebuilt)

	a.c.natsClient = unpublishableClient(t)
	a.c.sweepExpiredApprovals(t.Context())

	require.Contains(t, a.bucket.written(), terminalMarkerKey(coldApprovalLoopID),
		"fixture: the sweep committed its marker before the publication failed")
	require.Equal(t, agentic.LoopStateAwaitingApproval, persistedLoop(t, a.bucket, coldApprovalLoopID).State,
		"the residual: the record stays gated")
	require.False(t, a.held(), "the failed commit released the loop")

	a.c.natsClient = nil
	settled, err := a.deliver(t, a.answerOf(agentic.ApprovalDecisionApprove))

	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, settled)
	requireLoopTimedOut(t, a)
}
