package agenticloop

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/c360studio/semstreams/processor/agentic-loop/prompt"
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
			e.TaskPrompt = original.Content
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
		require.NotNil(t, settled.CompletionState)
		assert.Equal(t, original.Content, settled.CompletionState.Prompt,
			"a rebuilt loop's completion publishes the prompt its record carries (was empty before #1365)")
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

	t.Run("an emptied context on the rebuilt loop recovers the record's prompt, then the uncarried turn", func(t *testing.T) {
		h := rebuildAcrossAReplacementWithoutAnswer(t, func(e *agentic.LoopEntity) {
			e.PublishedRequestID = req(2)
			e.Iterations = 1
			e.TaskPrompt = original.Content
			e.PendingContinuation = true
			e.PendingContinuationPrompt = deferredTurn
		}, req(2), []agentic.ChatMessage{system, original})

		// A fresh manager is the emptied context: GC/repair left nothing.
		emptied := NewContextManager(rebuildLoopID, "test-model", h.loopManager.contextConfig)
		recovered := h.recoverEmptyContext(rebuildLoopID, emptied, 2, 0)

		var users []string
		for _, m := range recovered {
			if m.Role == "user" {
				users = append(users, m.Content)
			}
		}
		require.Len(t, users, 2, "the birth prompt, then the uncarried turn")
		assert.Contains(t, users[0], "Original task: "+original.Content,
			"recovery re-injects the record's prompt, not the placeholder")
		assert.NotContains(t, users[0], "Continue with the task.")
		assert.Equal(t, deferredTurn, users[1],
			"the uncarried turn is in no request once the context is gone; recovery must carry it")
	})
}

// TestTheAgenticTierCarriesADeferredTurnPastARebuiltToolCall is W-b as the
// agentic E2E tier runs it (#1365, task 3.7): the loop is rebuilt under the
// configuration the tier ships — configs/agentic.json, through the production
// loader, the component's own config resolution and the model registry wired
// the way NewComponent wires it — and R1 is answered with a TOOL CALL, so the
// turn has to survive that answer's handling and the tool result before the
// request that carries it is built.
//
// TestARebuiltLoopCarriesTheTurnItsRecordAccepted drives the same rebuild
// seams, but under DefaultConfig with no registry: every model falls back to
// DefaultContextLimit and compaction never fires. The tier's mock endpoint once
// declared a 4096-token window against the 4000-token headroom floor, so every
// model answer compacted RegionRecentHistory — the birth prompt and the
// replayed turn with it — into a summary, and the request after the tool
// result carried neither. The unit harness was green and the tier was red on
// the same seams; this is the harness with the tier's configuration.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestTheAgenticTierCarriesADeferredTurnPastARebuiltToolCall(t *testing.T) {
	// Read here and loaded through the production merge: the loader's own
	// file path refuses a path outside the working directory.
	raw, err := os.ReadFile("../../configs/agentic.json")
	require.NoError(t, err)
	shipped, err := config.NewLoader().LoadFromBytes(raw)
	require.NoError(t, err)
	require.NotNil(t, shipped.ModelRegistry, "the tier config must declare the registry its loops resolve against")
	loopConfig, _, _, err := resolveConfig(shipped.Components["agentic-loop"].Config)
	require.NoError(t, err)
	tierModel := shipped.ModelRegistry.Defaults.Model
	require.NotEmpty(t, tierModel)

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	h := NewMessageHandler(loopConfig, WithLoopManagerLogger(logger),
		WithLoopManagerModelRegistry(shipped.ModelRegistry))
	h.modelRegistry = shipped.ModelRegistry
	c := releaseTestComponent(t, h)
	c.logger = logger
	c.loopsBucket = &recordingLoopBucket{}

	// R1 as the predecessor process built it: the birth under the same
	// configuration, with the prompt registry initPromptRegistry seeds at
	// Start. Its conversation is what the stream retains and the rebuild
	// replays; a hand-written one sizes the context by the fixture's choice.
	birth := agentic.ChatMessage{Role: "user", Content: "look in the first drawer"}
	predecessor := NewMessageHandler(loopConfig, WithLoopManagerModelRegistry(shipped.ModelRegistry))
	predecessor.modelRegistry = shipped.ModelRegistry
	prompts := prompt.NewRegistry()
	prompts.AddAll(prompt.DefaultFragments())
	predecessor.SetPromptRegistry(prompts)
	born, err := predecessor.HandleTask(t.Context(), TaskMessage{
		LoopID: rebuildLoopID, TaskID: "task-tier", Role: "general", Model: tierModel, Prompt: birth.Content,
	})
	require.NoError(t, err)
	first := oneRetainedRequest(t, born)
	require.Equal(t, looprequest.ID{LoopID: rebuildLoopID, Iteration: 1, Retry: 0}.String(), first.RequestID)
	c.requestEvidence = stubEvidenceReader{requestID: first.RequestID, messages: first.Messages}
	coldRecord(t, c, rebuildLoopID, func(e *agentic.LoopEntity) {
		e.Model = tierModel
		e.PublishedRequestID = first.RequestID
		e.Iterations = 0
		e.TaskPrompt = birth.Content
		e.PendingContinuation = true
		e.PendingContinuationPrompt = deferredTurn
	})

	adopted, err := c.adoptNewerRetainedRequest(t.Context(), rebuildLoopID)
	require.NoError(t, err)
	require.NoError(t, c.restoreLoopFromEvidence(t.Context(), rebuildLoopID, adopted, ""))
	require.Contains(t, logs.String(), "replayed the deferred turn")

	call := agentic.ToolCall{ID: "call-tier", Name: "query_entity", Arguments: map[string]any{"entity_id": "acme.ops.semstreams.agentic.sensor.drawer"}}
	dispatched, err := h.HandleModelResponse(t.Context(), rebuildLoopID, agentic.AgentResponse{
		RequestID:    first.RequestID,
		Status:       agentic.StatusToolCall,
		FinishReason: "tool_calls",
		Message:      agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{call}},
	})
	require.NoError(t, err)
	require.Equal(t, -1, turnsInMintedRequest(t, dispatched, deferredTurn),
		"a tool-call answer mints no request; the tools have not answered")

	answered, err := h.HandleToolResult(t.Context(), rebuildLoopID, agentic.ToolResult{
		CallID:      call.ID,
		Name:        call.Name,
		Content:     "the drawer is empty",
		LoopID:      rebuildLoopID,
		RequestID:   first.RequestID,
		ExecutionID: deriveToolExecutionID(first.RequestID, call.ID, 1),
		CallOrdinal: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, turnsInMintedRequest(t, answered, deferredTurn),
		"the request after the tool result must ask the replayed turn once; 0 is the turn compacted away "+
			"under the tier's context budget")
	assert.Equal(t, 1, turnsInMintedRequest(t, answered, birth.Content),
		"the request after the tool result must still carry the birth prompt once")
}

// oneRetainedRequest decodes the one agent.request a result minted: what the
// stream retains for a replacement to rebuild from.
func oneRetainedRequest(t *testing.T, result HandlerResult) agentic.AgentRequest {
	t.Helper()
	var requests []agentic.AgentRequest
	for _, msg := range result.PublishedMessages {
		if !strings.Contains(msg.Subject, "agent.request") {
			continue
		}
		var envelope struct {
			Payload agentic.AgentRequest `json:"payload"`
		}
		require.NoError(t, json.Unmarshal(msg.Data, &envelope), "decode agent.request on %s", msg.Subject)
		requests = append(requests, envelope.Payload)
	}
	require.Len(t, requests, 1, "the birth mints exactly one request")
	return requests[0]
}

// rebuildAcrossAReplacementWithoutAnswer is rebuildAcrossAReplacement stopped
// before the model's answer is applied.
func rebuildAcrossAReplacementWithoutAnswer(
	t *testing.T,
	shape func(*agentic.LoopEntity),
	retainedID string,
	retained []agentic.ChatMessage,
) *MessageHandler {
	t.Helper()
	h := NewMessageHandler(DefaultConfig())
	c := releaseTestComponent(t, h)
	c.loopsBucket = &recordingLoopBucket{}
	c.requestEvidence = stubEvidenceReader{requestID: retainedID, messages: retained}
	coldRecord(t, c, rebuildLoopID, shape)
	adopted, err := c.adoptNewerRetainedRequest(t.Context(), rebuildLoopID)
	require.NoError(t, err)
	require.NoError(t, c.restoreLoopFromEvidence(t.Context(), rebuildLoopID, adopted, ""))
	return h
}

// TestTheLoopsPromptIsTheOneThatBoreIt is task_prompt's write (#1365, OQ5
// (a′)): the birth write carries the prompt of the task that bore the loop, and
// a continuation — deferred and then carried — never rewrites it, so the
// continued loop's completion publishes the BIRTH prompt.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestTheLoopsPromptIsTheOneThatBoreIt(t *testing.T) {
	const loopID = "9a1b2c3d-4e5f-4a6b-8c7d-0e1f2a3b4c5d"
	const birthPrompt, turnPrompt = "look in the first drawer", "and also check the second drawer"

	t.Run("the birth record carries the prompt", func(t *testing.T) {
		c := evidenceComponent(t, "")
		c.natsClient = unpublishableClient(t)
		deliverBirth(t, c, loopID)
		require.Equal(t, "first turn", decodeRecord(t, c, loopID).TaskPrompt,
			"the birth write must put the task's prompt on the record, or a rebuild has none to restore")
	})

	t.Run("a continued loop's completion carries the birth prompt", func(t *testing.T) {
		h := NewMessageHandler(DefaultConfig())
		birth, err := h.HandleTask(t.Context(), TaskMessage{
			TaskID: "task-bore", LoopID: loopID, Role: "general", Model: "model-a", Prompt: birthPrompt,
		})
		require.NoError(t, err)
		r1 := mintedRequestIDsFromResult(t, birth)
		require.Len(t, r1, 1)
		require.NoError(t, h.loopManager.SetPublishedRequest(loopID, r1[0]))

		deferred, err := h.HandleTask(t.Context(), TaskMessage{
			TaskID: "task-continued", LoopID: loopID, Role: "general", Model: "model-a", Prompt: turnPrompt,
		})
		require.NoError(t, err)
		require.True(t, deferred.Deferred)

		advanced, err := h.HandleModelResponse(t.Context(), loopID, agentic.AgentResponse{
			RequestID: r1[0], Status: agentic.StatusComplete,
			Message: agentic.ChatMessage{Role: "assistant", Content: "the first drawer is empty"},
		})
		require.NoError(t, err)
		r2 := mintedRequestIDsFromResult(t, advanced)
		require.Len(t, r2, 1, "the deferred turn advances the loop")
		require.NoError(t, h.loopManager.SetPublishedRequest(loopID, r2[0]))

		done, err := h.HandleModelResponse(t.Context(), loopID, agentic.AgentResponse{
			RequestID: r2[0], Status: agentic.StatusComplete,
			Message: agentic.ChatMessage{Role: "assistant", Content: "both drawers are empty"},
		})
		require.NoError(t, err)
		require.NotNil(t, done.CompletionState)
		assert.Equal(t, birthPrompt, done.CompletionState.Prompt,
			"a continuation's turn became the loop's prompt; the record stores the turn twice and the "+
				"event names the wrong task")
	})
}
