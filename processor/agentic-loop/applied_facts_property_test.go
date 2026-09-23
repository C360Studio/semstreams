package agenticloop

import (
	"context"
	"encoding/json"
	"testing"

	"pgregory.net/rapid"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
)

// appliedFactsStream is the durable half of a loop's world: the AgentRequests
// the stream retains for it, and how many times each was published.
//
// It is a model of JetStream retention, not of the loop: nothing here decides
// anything. The decisions under test — publish or adopt, adopt-then-classify —
// are made by the production functions the actions below call.
type appliedFactsStream struct {
	loopID    string
	retained  []string
	publishes map[string]int
}

func (s *appliedFactsStream) publish(requestID string) {
	s.retained = append(s.retained, requestID)
	s.publishes[requestID]++
}

func (s *appliedFactsStream) holds(requestID string) bool {
	for _, id := range s.retained {
		if id == requestID {
			return true
		}
	}
	return false
}

func (s *appliedFactsStream) newest() (string, bool) {
	if len(s.retained) == 0 {
		return "", false
	}
	return s.retained[len(s.retained)-1], true
}

// ReadRetainedRequest serves the production evidence seam from the model, so
// every read under test is the real one.
func (s *appliedFactsStream) ReadRetainedRequest(context.Context, string, string) ([]byte, bool, error) {
	newest, ok := s.newest()
	if !ok {
		return nil, false, nil
	}
	request := agentic.AgentRequest{
		RequestID: newest,
		LoopID:    s.loopID,
		Messages:  []agentic.ChatMessage{{Role: "user", Content: "retained"}},
	}
	data, err := json.Marshal(message.NewBaseMessage(request.Schema(), &request, "test"))
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// ReadRetainedResponse answers nothing: this model retains requests, and the
// invariants below are stated over requests and the applied set. The tool
// batch a response carries belongs to the cold rebuild, which no action here
// drives.
func (s *appliedFactsStream) ReadRetainedResponse(context.Context, string, string) ([]byte, bool, error) {
	return nil, false, nil
}

// TestPropAppliedFactsHoldAcrossEveryCrashWindow drives the loop record
// through advances, lost record writes (W4), process replacements and
// redeliveries in arbitrary order, and checks after every single step the
// invariants this model actually reaches.
//
// They are the ones the change's own requirement states, not ones read back
// out of the implementation:
//
//   - I1 a record naming R means the stream retains R.
//   - I3 iterations moves only with published_request_id — checked in its
//     derived form, iterations == the named request's iteration minus one,
//     which is what every writer must maintain.
//   - The publication property: no request is ever published twice while the
//     stream still holds it.
//
// I2 and I4 are deliberately NOT claimed here, and the two checks that
// claimed them were DELETED rather than left standing (owner ruling
// 2026-09-23 on #1330, round-2 docket question 2 — NARROW). Neither could
// fire: the advance action drains the applied set and writes the advanced
// record with it empty, so the durable set between actions is always empty
// and the I2 loop never reached an assertion; and no action here creates an
// approval gate, so the I4 branch was never true. A check that cannot fire is
// a coverage claim written in code, which is worse than no claim at all.
//
// What those two invariants have instead is named examples, each driving the
// real production path:
//
//   - I2 (membership) — TestToolResultRedeliveredToAReplacementProcess's
//     "W2" arm and TestAReplayedAppliedToolResultDoesNotQuarantineItsLane in
//     tool_result_redelivery_integration_test.go,
//     TestATaskRedeliveredOverAProgressedFirstBatchIsNotRepublished in
//     task_redelivery_integration_test.go, and
//     TestARestoredToolBatchKnowsWhatIsLeftToRun and
//     TestAColdToolResultRebuildsTheBatchItBelongsTo in loop_rebuild_test.go.
//   - I4 (a gate names the published request) — the one write in L4a that
//     touches a gate is step 0's adopt, pinned by
//     TestColdReadAdoptsTheNewestRetainedRequestFirst's "a pending approval
//     gate is cleared in the same write" arm (loop_carrier_test.go) on the
//     DURABLE record, and by TestAnAdoptedRequestClearsTheGateItAdvancedPast
//     (loop_record_writer_test.go) on the record the adopt RETURNS, which is
//     what the classification running next reads.
//
// Boundary coverage is by construction: the advance action draws its own
// crash flag, so "the record write never landed" is an ordinary draw rather
// than a lucky one, and the replacement action empties process memory so the
// cold arms run against a record no live memory can repair.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestPropAppliedFactsHoldAcrossEveryCrashWindow(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		const loopID = "1f2e3d4c-5b6a-4079-8899-aabbccddeeff"
		stream := &appliedFactsStream{
			loopID:    loopID,
			publishes: map[string]int{},
		}
		bucket := &recordingLoopBucket{}
		ctx := context.Background()

		// Birth, through the production path: the record is written before
		// the first request is published (owner ruling Q1).
		h := NewMessageHandler(DefaultConfig())
		c := releaseTestComponent(t, h)
		c.loopsBucket = bucket
		c.requestEvidence = stream
		if _, err := h.loopManager.CreateLoopWithID(loopID, "task-prop", "general", "model", 10000); err != nil {
			rt.Fatalf("create loop: %v", err)
		}
		first := h.loopManager.GenerateRequestID(loopID)
		if err := h.loopManager.SetPublishedRequest(loopID, first); err != nil {
			rt.Fatalf("name the first request: %v", err)
		}
		if err := c.createLoopState(ctx, loopID); err != nil {
			rt.Fatalf("write the birth record: %v", err)
		}
		stream.publish(first)

		holdsLoop := func() bool {
			_, err := h.loopManager.GetLoop(loopID)
			return err == nil
		}
		record := func() agentic.LoopEntity {
			raw, ok := bucket.value(loopID)
			if !ok {
				rt.Fatalf("the loop has no record at all")
			}
			var entity agentic.LoopEntity
			if err := json.Unmarshal(raw, &entity); err != nil {
				rt.Fatalf("decode record: %v", err)
			}
			return entity
		}
		replaceProcess := func() {
			h = NewMessageHandler(DefaultConfig())
			c = releaseTestComponent(t, h)
			c.loopsBucket = bucket
			c.requestEvidence = stream
		}

		rt.Repeat(map[string]func(*rapid.T){
			"advance the loop": func(rt *rapid.T) {
				if !holdsLoop() {
					rt.Skip("a process with no memory of the loop cannot advance it")
				}
				crashBeforeTheWrite := rapid.Bool().Draw(rt, "crashBeforeTheWrite")
				withToolBatch := rapid.Bool().Draw(rt, "withToolBatch")

				current := record().PublishedRequestID
				if withToolBatch {
					// One dispatched execution of the CURRENT request, stored
					// the way the handler stores it. The advance drains it.
					execution := deriveToolExecutionID(current, "call-prop", 1)
					if err := h.loopManager.StoreToolResult(loopID, agentic.ToolResult{
						CallID: "call-prop", ExecutionID: execution, RequestID: current, CallOrdinal: 1,
					}); err != nil {
						rt.Fatalf("store tool result: %v", err)
					}
					if !crashBeforeTheWrite {
						// The applied set is durable until the advance drains
						// it, exactly as production leaves it mid-batch.
						if err := c.persistLoopState(ctx, loopID); err != nil {
							rt.Fatalf("persist the applied set: %v", err)
						}
					}
				}

				// The advance, in production order: the batch is drained into
				// the conversation, the iteration moves, the next request is
				// minted, named, published — and only then is the record
				// written.
				h.loopManager.GetAndClearToolResults(loopID)
				if err := h.loopManager.IncrementIteration(loopID); err != nil {
					rt.Fatalf("increment iteration: %v", err)
				}
				next := h.loopManager.GenerateRequestID(loopID)
				if err := h.loopManager.SetPublishedRequest(loopID, next); err != nil {
					rt.Fatalf("name the next request: %v", err)
				}
				adopted, err := c.adoptRetainedRequest(ctx, loopID, next)
				if err != nil {
					rt.Fatalf("decide the minted request: %v", err)
				}
				if !adopted {
					stream.publish(next)
				}
				if crashBeforeTheWrite {
					// W4: the request is retained and the record does not name
					// it. The process that owed that write is gone.
					replaceProcess()
					return
				}
				if err := c.persistLoopState(ctx, loopID); err != nil {
					rt.Fatalf("write the advanced record: %v", err)
				}
			},

			"replay the mint of the current request": func(rt *rapid.T) {
				// W3: the write landed and the delivery was not acknowledged,
				// so the same mint runs again in the process that made it. It
				// must adopt what the stream already holds rather than publish
				// a second copy. The name replayed is the one this process
				// minted — the in-memory entity's — because that is what a
				// re-run of the handler reaches for.
				if !holdsLoop() {
					rt.Skip("a replaced process replays no mint of its predecessor")
				}
				held, err := h.loopManager.GetLoop(loopID)
				if err != nil {
					rt.Fatalf("read the held loop: %v", err)
				}
				current := held.PublishedRequestID
				adopted, err := c.adoptRetainedRequest(ctx, loopID, current)
				if err != nil {
					rt.Fatalf("replay the mint: %v", err)
				}
				if !adopted {
					stream.publish(current)
				}
			},

			"replace the process": func(_ *rapid.T) {
				replaceProcess()
			},

			"redeliver a tool result to a cold process": func(rt *rapid.T) {
				if holdsLoop() {
					rt.Skip("this action is the cold arm; the process still holds the loop")
				}
				which := rapid.IntRange(0, len(stream.retained)-1).Draw(rt, "requestOfTheRedeliveredResult")
				requestID := stream.retained[which]
				_ = c.handleToolResultMessage(ctx, baseMessageBytes(t, &agentic.ToolResult{
					CallID: loopID + ":tool:prop", Name: "search", Content: "result",
					LoopID: loopID, RequestID: requestID,
				}))
			},

			"redeliver a model response to a cold process": func(rt *rapid.T) {
				if holdsLoop() {
					rt.Skip("this action is the cold arm; the process still holds the loop")
				}
				which := rapid.IntRange(0, len(stream.retained)-1).Draw(rt, "requestOfTheRedeliveredResponse")
				_ = c.handleResponseMessage(ctx, baseMessageBytes(t, &agentic.AgentResponse{
					RequestID: stream.retained[which], Status: agentic.StatusComplete,
					Message: agentic.ChatMessage{Role: "assistant", Content: "an answer"},
				}))
			},

			"": func(rt *rapid.T) {
				entity := record()

				// I1: the record never names a request the stream does not hold.
				if entity.PublishedRequestID == "" {
					rt.Fatalf("a non-terminal record names no request at all")
				}
				if !stream.holds(entity.PublishedRequestID) {
					rt.Fatalf("I1: the record names %q, which the stream does not retain",
						entity.PublishedRequestID)
				}

				// I3, in the form every writer must maintain.
				named, err := looprequest.Parse(entity.PublishedRequestID)
				if err != nil {
					rt.Fatalf("the record names an unparseable request %q: %v", entity.PublishedRequestID, err)
				}
				if named.LoopID != loopID {
					rt.Fatalf("the record names another loop's request %q", entity.PublishedRequestID)
				}
				if entity.Iterations != named.Iteration-1 {
					rt.Fatalf("I3: iterations=%d beside request %q, whose iteration is %d",
						entity.Iterations, entity.PublishedRequestID, named.Iteration)
				}

				// I2 and I4 are not checked here: no action of this model
				// leaves a populated applied set between steps or creates an
				// approval gate, so both checks were unfirable. Their named
				// examples are listed in this test's doc comment.

				// The publication property: a request the stream already holds
				// is adopted, never published a second time.
				for requestID, count := range stream.publishes {
					if count > 1 {
						rt.Fatalf("request %q was published %d times while the stream held it",
							requestID, count)
					}
				}
			},
		})
	})
}
