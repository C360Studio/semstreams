package agenticloop

import (
	"context"
	"encoding/json"
	"testing"

	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/stretchr/testify/require"
)

// unpublishableClient is a real client that was never connected, so every
// publish genuinely fails on the production path. It is how the two carrier
// orders are told apart without a broker: whichever step runs first is the one
// whose failure stops the other from happening.
func unpublishableClient(t *testing.T) *natsclient.Client {
	t.Helper()
	client, err := natsclient.NewClient("nats://127.0.0.1:1")
	require.NoError(t, err)
	return client
}

// carrierLoop births a loop and gives it a record, exactly as the task lane
// does, so the component holds the revision the carrier compares against.
func carrierLoop(t *testing.T) (*Component, *recordingLoopBucket, string) {
	t.Helper()
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-carrier", "general", "model", 5)
	require.NoError(t, err)
	c := releaseTestComponent(t, handler)
	bucket := &recordingLoopBucket{}
	c.loopsBucket = bucket
	seedLoopRecord(t, c, loopID)
	return c, bucket, loopID
}

func nonTerminalResultWithAPublication(loopID string) HandlerResult {
	return HandlerResult{
		LoopID: loopID,
		State:  agentic.LoopStateExecuting,
		PublishedMessages: []PublishedMessage{{
			Subject: "agent.request." + loopID,
			Data:    []byte(`{"request":true}`),
		}},
	}
}

// TestCarrierOrderDecidesWhatAFailedPublishLeavesBehind is the order assertion
// for both lanes at once, at the carrier seam that owns the choice.
//
// publishThenWrite is the L4a order for a non-terminal result on the
// model-response and tool-result lanes: the publication runs first, so a
// publish that did not commit leaves NO record behind. writeThenPublish is
// what the approval lane and the approval-timeout sweeper keep until #1362:
// the record is written first, so the same failure leaves it committed.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestCarrierOrderDecidesWhatAFailedPublishLeavesBehind(t *testing.T) {
	t.Run("publish then write leaves no record", func(t *testing.T) {
		c, bucket, loopID := carrierLoop(t)
		c.natsClient = unpublishableClient(t)

		err := c.persistHandlerResult(t.Context(), nonTerminalResultWithAPublication(loopID), publishThenWrite)
		require.Error(t, err)
		require.True(t, errs.IsFatal(err), "a publish of unknown durability is commit-unknown")
		require.Empty(t, bucket.written(),
			"the publish ran first, so its failure must have stopped the record write")
	})

	t.Run("write then publish commits the record", func(t *testing.T) {
		c, bucket, loopID := carrierLoop(t)
		c.natsClient = unpublishableClient(t)

		err := c.persistHandlerResult(t.Context(), nonTerminalResultWithAPublication(loopID), writeThenPublish)
		require.Error(t, err)
		require.Equal(t, []string{loopID}, bucket.written(),
			"the record ran first, so it is committed even though the publish was not")
	})
}

// TestApprovalLaneKeepsWriteThenPublish pins the CALL SITE, not the carrier:
// the approval lane's own settlement must ask for write-then-publish until
// #1362 moves it. A carrier that honours both orders proves nothing about
// which one this lane passes.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestApprovalLaneKeepsWriteThenPublish(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-approval-order", "general", "model", 5)
	require.NoError(t, err)

	entity, err := handler.GetLoop(loopID)
	require.NoError(t, err)
	require.NoError(t, entity.BeginAwaitingApproval(
		"call-approval", "search", map[string]any{"q": "x"}, "approval_required: needs a human", 0, ""))
	require.NoError(t, handler.loopManager.UpdateLoop(entity))

	c := releaseTestComponent(t, handler)
	bucket := &recordingLoopBucket{}
	c.loopsBucket = bucket
	seedLoopRecord(t, c, loopID)
	c.natsClient = unpublishableClient(t)

	response := &agentic.ApprovalResponse{
		LoopID: loopID, CallID: "call-approval",
		Decision: agentic.ApprovalDecisionApprove, ApprovedBy: "operator",
		DecidedAt: time.Now().UTC(),
	}
	decision, err := c.handleApprovalResponseMessage(t.Context(), baseMessageBytes(t, response))
	require.Error(t, err, "the unconnected publish must fail so the order is observable")
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
	require.Equal(t, []string{loopID}, bucket.written(),
		"the approval lane must still write its record before it publishes (#1362 moves it)")
}

// TestCarrierCompareAndSwapLossRetriesAndReleasesTheLoop: the record moved, so
// this process is holding a loop somebody else advanced. It must not retry in
// place against its own stale view — it releases the loop and returns the
// delivery transient, so the redelivery re-enters cold against the record that
// won. Without the release the loser keeps a stale conversation forever.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestCarrierCompareAndSwapLossRetriesAndReleasesTheLoop(t *testing.T) {
	c, bucket, loopID := carrierLoop(t)

	// A foreign writer moves the record, exactly as a second consumer would.
	_, err := bucket.Put(t.Context(), loopID, []byte(`{"id":"other"}`))
	require.NoError(t, err)

	err = c.persistLoopState(t.Context(), loopID)
	require.ErrorIs(t, err, natsclient.ErrKVRevisionMismatch)

	_, getErr := c.handler.GetLoop(loopID)
	require.Error(t, getErr, "a process that lost the record must not keep the loop in memory")
	_, held := c.observedLoopRevision(loopID)
	require.False(t, held, "the revision goes with the loop it belonged to")
}

// TestBirthRefusesASecondCreateForTheSameLoop: birth is by Create, so a second
// consumer racing the same loop ID is refused instead of overwriting a record
// that may already carry iterations, a published request and an applied set.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestBirthRefusesASecondCreateForTheSameLoop(t *testing.T) {
	c, bucket, loopID := carrierLoop(t)
	require.Equal(t, []byte(nil), mustNotExist(bucket, "COMPLETE_"+loopID))

	err := c.createLoopState(t.Context(), loopID)
	require.ErrorIs(t, err, natsclient.ErrKVKeyExists)
}

func mustNotExist(bucket *recordingLoopBucket, key string) []byte {
	value, ok := bucket.value(key)
	if !ok {
		return nil
	}
	return value
}

// TestMintedRequestAdoptsAnAlreadyRetainedIdentity walks every arm of the
// pre-publish identity check (owner ruling Q4): the request is already
// retained under this exact name (adopt, publish nothing), the stream holds an
// older request or none (publish), and the stream holds a request BEYOND the
// one being minted (quarantine — some other writer advanced this loop).
//
// spec: agentic-loop / The loop record names its outstanding request
func TestMintedRequestAdoptsAnAlreadyRetainedIdentity(t *testing.T) {
	loopID := "1a5ba1b7-1f2b-4a2f-9f8a-2a52e2f5f9aa"
	minting := looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()

	cases := map[string]struct {
		retained  string
		wantAdopt bool
		wantErr   bool
	}{
		"already retained under this identity": {retained: minting, wantAdopt: true},
		"stream holds the previous request":    {retained: looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 0}.String()},
		"stream holds a retry of the previous": {retained: looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 1}.String()},
		"stream holds nothing":                 {retained: ""},
		"stream holds a later request":         {retained: looprequest.ID{LoopID: loopID, Iteration: 3, Retry: 0}.String(), wantErr: true},
		"stream holds another loop's request":  {retained: "other:req:1:0", wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := evidenceComponent(t, tc.retained)
			adopted, err := c.adoptRetainedRequest(t.Context(), loopID, minting)
			if tc.wantErr {
				require.Error(t, err)
				require.True(t, errs.IsFatal(err), "a conflicting retained request quarantines")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantAdopt, adopted)
		})
	}
}

// TestColdReadAdoptsTheNewestRetainedRequestFirst is step 0: before any
// redelivered input is classified, the record is made to name the loop's
// newest retained request. The crash window is W4 — the predecessor published
// the next request and died before the record update — and classifying against
// the stale record there re-applies an input the loop has moved past.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestColdReadAdoptsTheNewestRetainedRequestFirst(t *testing.T) {
	loopID := "1a5ba1b7-1f2b-4a2f-9f8a-2a52e2f5f9aa"

	t.Run("a newer retained request is adopted with its iteration and an empty applied set", func(t *testing.T) {
		retained := looprequest.ID{LoopID: loopID, Iteration: 3, Retry: 0}.String()
		c := evidenceComponent(t, retained)
		record := coldRecord(t, c, loopID, func(e *agentic.LoopEntity) {
			e.PublishedRequestID = looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()
			e.Iterations = 1
			e.PendingToolResults = map[string]agentic.ToolResult{"exec-1": {ExecutionID: "exec-1"}}
		})

		require.NoError(t, c.adoptNewerRetainedRequest(t.Context(), loopID, record))

		written := decodeRecord(t, c, loopID)
		require.Equal(t, retained, written.PublishedRequestID)
		require.Equal(t, 2, written.Iterations, "a request's iteration ordinal is the record's plus one")
		require.Empty(t, written.PendingToolResults, "the advance drains the applied set before it mints")
		require.NoError(t, written.Validate())
	})

	t.Run("a pending approval gate is cleared in the same write", func(t *testing.T) {
		retained := looprequest.ID{LoopID: loopID, Iteration: 3, Retry: 0}.String()
		c := evidenceComponent(t, retained)
		gate := looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()
		record := coldRecord(t, c, loopID, func(e *agentic.LoopEntity) {
			e.PublishedRequestID = gate
			e.Iterations = 1
			e.State = agentic.LoopStateAwaitingApproval
			e.StateBeforeApproval = agentic.LoopStateExecuting
			e.PendingApproval = &agentic.PendingApprovalState{RequestID: gate, CallID: "call-1", ToolName: "t"}
		})

		require.NoError(t, c.adoptNewerRetainedRequest(t.Context(), loopID, record))

		written := decodeRecord(t, c, loopID)
		require.Equal(t, retained, written.PublishedRequestID)
		require.Nil(t, written.PendingApproval, "a request newer than the gate was minted after the gate's batch closed")
		require.Equal(t, agentic.LoopStateExecuting, written.State)
	})

	t.Run("the current request writes nothing", func(t *testing.T) {
		current := looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()
		c := evidenceComponent(t, current)
		record := coldRecord(t, c, loopID, func(e *agentic.LoopEntity) {
			e.PublishedRequestID = current
			e.Iterations = 1
		})
		bucket := c.loopsBucket.(*recordingLoopBucket)
		bucket.resetWritten()

		require.NoError(t, c.adoptNewerRetainedRequest(t.Context(), loopID, record))
		require.Empty(t, bucket.written(), "an already-current record is not rewritten")
	})

	t.Run("no retained request writes nothing", func(t *testing.T) {
		c := evidenceComponent(t, "")
		record := coldRecord(t, c, loopID, func(e *agentic.LoopEntity) {
			e.PublishedRequestID = looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()
			e.Iterations = 1
		})
		bucket := c.loopsBucket.(*recordingLoopBucket)
		bucket.resetWritten()

		require.NoError(t, c.adoptNewerRetainedRequest(t.Context(), loopID, record))
		require.Empty(t, bucket.written())
	})

	t.Run("a record naming a request the stream never retained quarantines", func(t *testing.T) {
		c := evidenceComponent(t, looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 0}.String())
		record := coldRecord(t, c, loopID, func(e *agentic.LoopEntity) {
			e.PublishedRequestID = looprequest.ID{LoopID: loopID, Iteration: 4, Retry: 0}.String()
			e.Iterations = 3
		})

		err := c.adoptNewerRetainedRequest(t.Context(), loopID, record)
		require.Error(t, err)
		require.True(t, errs.IsFatal(err))
	})

	t.Run("an unparseable retained request quarantines", func(t *testing.T) {
		c := evidenceComponent(t, "not-a-request-id")
		record := coldRecord(t, c, loopID, func(e *agentic.LoopEntity) {
			e.PublishedRequestID = looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 0}.String()
		})

		err := c.adoptNewerRetainedRequest(t.Context(), loopID, record)
		require.Error(t, err)
		require.True(t, errs.IsFatal(err))
	})

	t.Run("an unparseable record request quarantines", func(t *testing.T) {
		c := evidenceComponent(t, looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String())
		record := coldRecord(t, c, loopID, func(e *agentic.LoopEntity) {
			e.PublishedRequestID = "not-a-request-id"
		})

		err := c.adoptNewerRetainedRequest(t.Context(), loopID, record)
		require.Error(t, err)
		require.True(t, errs.IsFatal(err))
	})
}

// stubEvidenceReader answers the retained-request read from a fixed body, so
// every arm of identity adoption is driven without a broker.
type stubEvidenceReader struct {
	requestID string
}

func (r stubEvidenceReader) ReadRetainedRequest(context.Context, string, string) ([]byte, bool, error) {
	if r.requestID == "" {
		return nil, false, nil
	}
	request := agentic.AgentRequest{
		RequestID: r.requestID,
		LoopID:    "unused",
		Messages:  []agentic.ChatMessage{{Role: "user", Content: "retained"}},
	}
	data, err := json.Marshal(message.NewBaseMessage(request.Schema(), &request, "test"))
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func evidenceComponent(t *testing.T, retainedRequestID string) *Component {
	t.Helper()
	handler := NewMessageHandler(DefaultConfig())
	c := releaseTestComponent(t, handler)
	c.loopsBucket = &recordingLoopBucket{}
	c.requestEvidence = stubEvidenceReader{requestID: retainedRequestID}
	return c
}

// coldRecord writes a record straight into the bucket and reads it back
// through the production revision-returning read, which is how a replacement
// process meets a loop it has no memory of.
func coldRecord(t *testing.T, c *Component, loopID string, shape func(*agentic.LoopEntity)) loopRecord {
	t.Helper()
	entity := agentic.NewLoopEntity(loopID, "task-cold", "general", "model", 10)
	entity.State = agentic.LoopStateExecuting
	shape(&entity)
	data, err := json.Marshal(entity)
	require.NoError(t, err)
	_, err = c.loopsBucket.Put(t.Context(), loopID, data)
	require.NoError(t, err)

	record := c.readLoopRecord(t.Context(), loopID)
	require.Equal(t, loopPresenceLive, record.presence)
	return record
}

func decodeRecord(t *testing.T, c *Component, loopID string) agentic.LoopEntity {
	t.Helper()
	entry, err := c.loopsBucket.Get(t.Context(), loopID)
	require.NoError(t, err)
	var entity agentic.LoopEntity
	require.NoError(t, json.Unmarshal(entry.Value(), &entity))
	return entity
}
