package agenticloop

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deferredTurn is the continuation's text in every window below. It shares no
// substring with the rest of the fixture conversation, so a count of it is a
// count of the turn and nothing else.
const deferredTurn = "and also check the second drawer"

// turnsInMintedRequest counts the messages carrying the deferred turn across
// every agent.request the result minted: the number of times the model is
// asked it. -1 when the result minted no request at all, so "not asked" and
// "nothing was sent" read differently.
func turnsInMintedRequest(t *testing.T, result HandlerResult, turn string) int {
	t.Helper()
	count, minted := 0, false
	for _, msg := range result.PublishedMessages {
		if !strings.Contains(msg.Subject, "agent.request") {
			continue
		}
		minted = true
		var envelope struct {
			Payload agentic.AgentRequest `json:"payload"`
		}
		require.NoError(t, json.Unmarshal(msg.Data, &envelope), "decode agent.request on %s", msg.Subject)
		for _, m := range envelope.Payload.Messages {
			if m.Role == "user" && m.Content == turn {
				count++
			}
		}
	}
	if !minted {
		return -1
	}
	return count
}

// turnsInContext counts the deferred turn in the rebuilt loop's conversation —
// what the next request built from it will carry.
func turnsInContext(h *MessageHandler, loopID, turn string) int {
	count := 0
	for _, m := range h.loopManager.GetContextManager(loopID).GetContext() {
		if m.Role == "user" && m.Content == turn {
			count++
		}
	}
	return count
}

// rebuildAcrossAReplacement is a replacement process meeting a loop it never
// held: the record the predecessor left is in the bucket, the stream retains
// retainedID with the given conversation, and the model's answer to
// retainedID arrives. The rebuild runs through the production seams, in the
// order the response lane runs them — adoption of the newest retained request
// first (adoptNewerRetainedRequest), then the rebuild from the record that
// adoption left (restoreLoopFromEvidence) — and the answer is applied by the
// production handler, whose result is what the carrier would publish.
func rebuildAcrossAReplacement(
	t *testing.T,
	shape func(*agentic.LoopEntity),
	retainedID string,
	retained []agentic.ChatMessage,
) (*MessageHandler, *bytes.Buffer, HandlerResult) {
	t.Helper()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	h := NewMessageHandler(DefaultConfig(), WithLoopManagerLogger(logger))
	c := releaseTestComponent(t, h)
	c.logger = logger
	c.loopsBucket = &recordingLoopBucket{}
	c.requestEvidence = stubEvidenceReader{requestID: retainedID, messages: retained}
	coldRecord(t, c, rebuildLoopID, shape)

	adopted, err := c.adoptNewerRetainedRequest(t.Context(), rebuildLoopID)
	require.NoError(t, err)
	require.Equal(t, retainedID, adopted.entity.PublishedRequestID,
		"fixture: the rebuild runs against the newest retained request")
	require.NoError(t, c.restoreLoopFromEvidence(t.Context(), rebuildLoopID, adopted, ""))

	answer, err := h.HandleModelResponse(t.Context(), rebuildLoopID, agentic.AgentResponse{
		RequestID: retainedID,
		Status:    agentic.StatusComplete,
		Message:   agentic.ChatMessage{Role: "assistant", Content: "the first drawer is empty"},
	})
	require.NoError(t, err)
	return h, &logs, answer
}

// TestARebuiltLoopCarriesTheTurnItsRecordAccepted is the deferred turn across a
// process replacement (#1365, design § 3.1): a continuation admitted while a
// request was outstanding is on the record as its marker AND its text, and the
// replacement's next request asks the model that turn — once, twice only in the
// one window the record cannot tell apart, never zero once the marker landed.
//
// Before #1365 the record held the marker only, the rebuild cleared it with a
// warning, and every window below asked the turn zero times.
//
// The five subtests are the five places a replacement can fall, by what the
// marker write left on the record and what the stream retains.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestARebuiltLoopCarriesTheTurnItsRecordAccepted(t *testing.T) {
	req := func(iteration int) string {
		return looprequest.ID{LoopID: rebuildLoopID, Iteration: iteration, Retry: 0}.String()
	}
	system := agentic.ChatMessage{Role: "system", Content: "you are a test agent"}
	original := agentic.ChatMessage{Role: "user", Content: "look in the first drawer"}
	interim := agentic.ChatMessage{Role: "assistant", Content: "opening the first drawer"}
	turn := agentic.ChatMessage{Role: "user", Content: deferredTurn}

	t.Run("W-a the marker write never landed: nothing to replay, the turn is not asked", func(t *testing.T) {
		h, logs, answer := rebuildAcrossAReplacement(t, func(e *agentic.LoopEntity) {
			e.PublishedRequestID = req(2)
			e.Iterations = 1
		}, req(2), []agentic.ChatMessage{system, original})

		assert.Zero(t, turnsInContext(h, rebuildLoopID, deferredTurn))
		assert.Equal(t, -1, turnsInMintedRequest(t, answer, deferredTurn),
			"a loop with nothing deferred settles on its completion and mints nothing")
		assert.True(t, answer.State.IsTerminal())
		assert.NotContains(t, logs.String(), "replayed the deferred turn")
	})

	t.Run("W-b the record names the request the turn deferred behind: carried once, then settled", func(t *testing.T) {
		h, logs, answer := rebuildAcrossAReplacement(t, func(e *agentic.LoopEntity) {
			e.PublishedRequestID = req(2)
			e.Iterations = 1
			e.PendingContinuation = true
			e.PendingContinuationPrompt = deferredTurn
		}, req(2), []agentic.ChatMessage{system, original})

		assert.Equal(t, 1, turnsInMintedRequest(t, answer, deferredTurn),
			"the completion of the request the turn deferred behind must advance and ask the turn once")
		carrier := mintedRequestIDsFromResult(t, answer)
		require.Len(t, carrier, 1)
		rebuilt, err := h.loopManager.GetLoop(rebuildLoopID)
		require.NoError(t, err)
		assert.Equal(t, carrier[0], rebuilt.PendingContinuationRequestID,
			"the request that carries the turn is named as its carrier")
		replayed := logLineContaining(t, logs.String(), "replayed the deferred turn")
		assert.Contains(t, replayed, "loop_id="+rebuildLoopID)
		assert.Contains(t, replayed, "retained_request_id="+req(2))

		// The carrier's own answer settles the loop, and the text clears with
		// the marker: a terminal record that still carried it would claim a
		// turn nobody owes.
		require.NoError(t, h.loopManager.SetPublishedRequest(rebuildLoopID, carrier[0]))
		settled, err := h.HandleModelResponse(t.Context(), rebuildLoopID, agentic.AgentResponse{
			RequestID: carrier[0],
			Status:    agentic.StatusComplete,
			Message:   agentic.ChatMessage{Role: "assistant", Content: "the second drawer is empty too"},
		})
		require.NoError(t, err)
		assert.True(t, settled.State.IsTerminal(), "the carrier's answer must settle the loop")
		after, err := h.loopManager.GetLoop(rebuildLoopID)
		require.NoError(t, err)
		assert.False(t, after.PendingContinuation)
		assert.Empty(t, after.PendingContinuationRequestID)
		assert.Empty(t, after.PendingContinuationPrompt,
			"the turn's text outlived the settle of the request that carried it")
	})

	t.Run("W-c a carrier minted after the turn, its record write lost: carried twice, logged", func(t *testing.T) {
		h, logs, answer := rebuildAcrossAReplacement(t, func(e *agentic.LoopEntity) {
			e.PublishedRequestID = req(2)
			e.Iterations = 1
			e.PendingContinuation = true
			e.PendingContinuationPrompt = deferredTurn
		}, req(3), []agentic.ChatMessage{system, original, interim, turn})

		// The accepted duplicate (design OQ4 (a)): the record cannot tell this
		// window from W-e, and a duplicate is degraded where a loss is not.
		assert.Equal(t, 2, turnsInMintedRequest(t, answer, deferredTurn),
			"the retained request already carried the turn and the replay adds it once more")
		replayed := logLineContaining(t, logs.String(), "replayed the deferred turn")
		assert.Contains(t, replayed, "loop_id="+rebuildLoopID)
		assert.Contains(t, replayed, "retained_request_id="+req(3),
			"the replay must name the adopted request, the other half an operator reads")
		assert.Equal(t, 2, turnsInContext(h, rebuildLoopID, deferredTurn))
	})

	t.Run("W-d the carrier's record write landed: nothing replayed, settled on its answer", func(t *testing.T) {
		h, logs, answer := rebuildAcrossAReplacement(t, func(e *agentic.LoopEntity) {
			e.PublishedRequestID = req(3)
			e.Iterations = 2
			e.PendingContinuation = true
			e.PendingContinuationRequestID = req(3)
			e.PendingContinuationPrompt = deferredTurn
		}, req(3), []agentic.ChatMessage{system, original, interim, turn})

		assert.Equal(t, 1, turnsInContext(h, rebuildLoopID, deferredTurn),
			"the retained carrier holds the turn once and the rebuild must not add it")
		assert.NotContains(t, logs.String(), "replayed the deferred turn")
		assert.Equal(t, -1, turnsInMintedRequest(t, answer, deferredTurn),
			"the carrier's answer is the turn's answer: nothing is re-asked")
		assert.True(t, answer.State.IsTerminal())
	})

	t.Run("W-e the turn deferred behind a request not yet on the record: the adopted request predates it, carried once", func(t *testing.T) {
		// The blocking finding of the design review: R2 was tracked as
		// outstanding and not yet on the record when the turn deferred behind
		// it, so the record names R1; R2 PubAck'd and the process died before
		// its record write. Adoption moves the record to R2 — built BEFORE the
		// turn — and leaves the marker as it found it. Naming R2 the carrier
		// there would skip the replay and let R2's answer settle the marker:
		// the turn lost with no warning at all.
		h, _, answer := rebuildAcrossAReplacement(t, func(e *agentic.LoopEntity) {
			e.PublishedRequestID = req(1)
			e.Iterations = 0
			e.PendingContinuation = true
			e.PendingContinuationPrompt = deferredTurn
		}, req(2), []agentic.ChatMessage{system, original, interim})

		assert.Equal(t, 1, turnsInContext(h, rebuildLoopID, deferredTurn))
		assert.Equal(t, 1, turnsInMintedRequest(t, answer, deferredTurn),
			"the adopted request was minted before the turn; the replay is its only copy")
	})
}
