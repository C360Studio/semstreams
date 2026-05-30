package agenticmodel

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model/wire/responses"
)

// streamResponses handles the Responses-native streaming path.
// Connection errors return a Go error (retryable); mid-stream
// errors return AgentResponse{Status:"error"} (not retryable).
// Mirrors streamChatCompletionWire's contract.
func (c *Client) streamResponses(ctx context.Context, rc *responses.Client, req responses.Request, requestID string) (agentic.AgentResponse, error) {
	stream, err := rc.ResponsesStream(ctx, &req)
	if err != nil {
		return agentic.AgentResponse{}, err
	}
	defer stream.Close()

	acc := responses.NewAccumulator()
	streamStart := time.Now()
	firstTokenRecorded := false

	for {
		ev, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// Mid-stream decode/IO error: surface as an error
			// AgentResponse with whatever was accumulated so the
			// loop can observe partial progress. Not retryable
			// (the retry loop classifies on Go errors, not on
			// error-status responses).
			final := acc.Final()
			resp := c.convertResponsesResponse(final, requestID)
			resp.Status = "error"
			resp.Error = err.Error()
			return resp, nil
		}

		if accErr := acc.Add(ev); accErr != nil && c.logger != nil {
			c.logger.Debug("responses accumulator: skipping malformed event",
				slog.String("request_id", requestID),
				slog.String("event_type", ev.Type),
				slog.Any("err", accErr))
		}

		c.dispatchResponsesChunk(ev, requestID)

		if c.metrics != nil {
			c.metrics.recordStreamChunk(req.Model)
			if !firstTokenRecorded && responsesEventCarriesText(ev) {
				c.metrics.recordStreamTTFT(req.Model, time.Since(streamStart).Seconds())
				firstTokenRecorded = true
			}
		}
	}

	if c.chunkHandler != nil {
		c.chunkHandler(StreamChunk{RequestID: requestID, Done: true})
	}

	final := acc.Final()
	return c.convertResponsesResponse(final, requestID), nil
}

// dispatchResponsesChunk forwards the per-event text deltas to the
// chunk handler. Translates the typed Responses event into the
// flat StreamChunk shape the rest of the agentic stack consumes.
// Non-text events (lifecycle, item lifecycle) are not forwarded —
// they don't carry visible content.
func (c *Client) dispatchResponsesChunk(ev *responses.Event, requestID string) {
	if c.chunkHandler == nil {
		return
	}
	switch ev.Type {
	case responses.EventTypeOutputTextDelta:
		c.chunkHandler(StreamChunk{
			RequestID:    requestID,
			ContentDelta: ev.Delta,
		})
	case responses.EventTypeReasoningSummaryTextDelta:
		c.chunkHandler(StreamChunk{
			RequestID:      requestID,
			ReasoningDelta: ev.Delta,
		})
	case responses.EventTypeFunctionCallArgumentsDelta:
		// Function-call arguments deltas flow through as content for
		// trace observability. The terminal accumulator still
		// reconstructs the full ToolCall from the .done snapshot.
		c.chunkHandler(StreamChunk{
			RequestID:    requestID,
			ContentDelta: ev.Delta,
		})
	}
}

// responsesEventCarriesText reports whether an event has non-empty
// delta text — used to gate the time-to-first-token metric so it
// fires on the first visible content rather than the first lifecycle
// event.
func responsesEventCarriesText(ev *responses.Event) bool {
	if ev == nil {
		return false
	}
	switch ev.Type {
	case responses.EventTypeOutputTextDelta,
		responses.EventTypeReasoningSummaryTextDelta,
		responses.EventTypeFunctionCallArgumentsDelta:
		return ev.Delta != ""
	}
	return false
}
