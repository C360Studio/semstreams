package responses

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// DefaultMaxFrameSize is the default per-SSE-frame buffer ceiling.
// Matches the model/wire ChatCompletion stream cap (16 MiB) —
// Responses output_text deltas are typically small but reasoning
// summary and function_call_arguments blobs can grow.
const DefaultMaxFrameSize = 16 * 1024 * 1024

// Event type constants enumerate the Responses SSE event types this
// package recognizes. The closed set covers function-calling +
// reasoning echo (Phase 1+2 scope); hosted-tool events (code
// interpreter, computer use, file search, etc.) are not modeled.
// Unknown event types decode into Event with Type populated and
// fields zero — callers can ignore them or surface them via Type.
const (
	// Lifecycle events: payload is {response: Response}.
	EventTypeResponseCreated    = "response.created"
	EventTypeResponseInProgress = "response.in_progress"
	EventTypeResponseCompleted  = "response.completed"
	EventTypeResponseFailed     = "response.failed"
	EventTypeResponseCancelled  = "response.cancelled"
	EventTypeResponseIncomplete = "response.incomplete"

	// Output item events: payload carries an OutputItem at OutputIndex.
	EventTypeOutputItemAdded = "response.output_item.added"
	EventTypeOutputItemDone  = "response.output_item.done"

	// Content part events: payload carries a ContentPart at
	// (OutputIndex, ContentIndex) inside a message item.
	EventTypeContentPartAdded = "response.content_part.added"
	EventTypeContentPartDone  = "response.content_part.done"

	// Streaming text events: delta accumulates, done snapshots final.
	EventTypeOutputTextDelta = "response.output_text.delta"
	EventTypeOutputTextDone  = "response.output_text.done"

	// Refusal events: same shape as text events but for refusals.
	EventTypeRefusalDelta = "response.refusal.delta"
	EventTypeRefusalDone  = "response.refusal.done"

	// Function-call arguments events: delta accumulates JSON-encoded
	// arguments string, done snapshots final.
	EventTypeFunctionCallArgumentsDelta = "response.function_call_arguments.delta"
	EventTypeFunctionCallArgumentsDone  = "response.function_call_arguments.done"

	// Reasoning summary part events: payload carries a SummaryPart at
	// (OutputIndex, SummaryIndex). Note Part is a RawMessage —
	// caller decodes via SummaryPart() helper since the "part" field
	// shape differs from content_part.* events.
	EventTypeReasoningSummaryPartAdded = "response.reasoning_summary_part.added"
	EventTypeReasoningSummaryPartDone  = "response.reasoning_summary_part.done"

	// Reasoning summary text events: delta accumulates, done
	// snapshots final.
	EventTypeReasoningSummaryTextDelta = "response.reasoning_summary_text.delta"
	EventTypeReasoningSummaryTextDone  = "response.reasoning_summary_text.done"

	// Error event: payload is {error: APIError}.
	EventTypeError = "error"
)

// Event is a typed Responses SSE event. The Type field is the
// discriminator; other fields are populated based on the variant.
// Use the helper methods (ContentPart, SummaryPart, APIError) to
// decode polymorphic fields by event type.
//
// Producer convention: the typed payload structs in OpenAI's
// documented shapes are flattened into this union for simplicity.
// Unknown event types still decode (Type populated, payload fields
// zero or RawMessage); callers iterate forward without error.
type Event struct {
	// Type is the SSE event name (e.g. "response.output_text.delta").
	// Mirrors the `event:` SSE line and the data payload's `type`
	// field, which the API contract holds equal.
	Type string `json:"type"`

	// SequenceNumber is the monotonically increasing event sequence
	// the API ships on every event. Useful for de-duplication on
	// reconnect; we don't reconnect (stateless one-shot stream) but
	// the field is carried for trace correlation.
	SequenceNumber int `json:"sequence_number,omitempty"`

	// Response carries the full Response on lifecycle events
	// (response.created, response.completed, response.failed, etc.).
	Response *Response `json:"response,omitempty"`

	// OutputIndex addresses the OutputItem inside Response.Output
	// that this event targets. Populated on item/content/text/refusal/
	// arguments/summary events.
	OutputIndex *int `json:"output_index,omitempty"`

	// ContentIndex addresses the ContentPart inside an OutputItem's
	// Content array. Populated on content_part.*, output_text.*,
	// refusal.* events.
	ContentIndex *int `json:"content_index,omitempty"`

	// SummaryIndex addresses the SummaryPart inside a reasoning
	// OutputItem's Summary array. Populated on reasoning_summary_*
	// events.
	SummaryIndex *int `json:"summary_index,omitempty"`

	// ItemID echoes the OutputItem.ID this event targets. Populated
	// on content/text/refusal/arguments/summary events.
	ItemID string `json:"item_id,omitempty"`

	// Item carries the OutputItem on output_item.added /
	// output_item.done.
	Item *OutputItem `json:"item,omitempty"`

	// Part carries the polymorphic part payload on content_part.* and
	// reasoning_summary_part.* events. Decode via ContentPart() or
	// SummaryPart() helpers based on Type.
	Part json.RawMessage `json:"part,omitempty"`

	// Delta accumulates partial text on output_text.delta,
	// refusal.delta, function_call_arguments.delta, and
	// reasoning_summary_text.delta events.
	Delta string `json:"delta,omitempty"`

	// Text snapshots final text on output_text.done and
	// reasoning_summary_text.done events.
	Text string `json:"text,omitempty"`

	// Arguments snapshots final JSON-encoded arguments on
	// function_call_arguments.done.
	Arguments string `json:"arguments,omitempty"`

	// Refusal snapshots final refusal body on refusal.done.
	Refusal string `json:"refusal,omitempty"`

	// Error carries the APIError on error events.
	Error json.RawMessage `json:"error,omitempty"`
}

// ContentPart decodes Part as a ContentPart. Only valid on
// content_part.* events; returns an error if Type is incompatible or
// Part is empty.
func (e *Event) ContentPart() (*ContentPart, error) {
	if e == nil {
		return nil, errors.New("responses: nil event")
	}
	if e.Type != EventTypeContentPartAdded && e.Type != EventTypeContentPartDone {
		return nil, fmt.Errorf("responses: ContentPart called on Event.Type=%q", e.Type)
	}
	if len(e.Part) == 0 {
		return nil, errors.New("responses: empty Part on content_part event")
	}
	var out ContentPart
	if err := json.Unmarshal(e.Part, &out); err != nil {
		return nil, fmt.Errorf("responses: decode ContentPart: %w", err)
	}
	return &out, nil
}

// SummaryPart decodes Part as a SummaryPart. Only valid on
// reasoning_summary_part.* events; returns an error if Type is
// incompatible or Part is empty.
func (e *Event) SummaryPart() (*SummaryPart, error) {
	if e == nil {
		return nil, errors.New("responses: nil event")
	}
	if e.Type != EventTypeReasoningSummaryPartAdded && e.Type != EventTypeReasoningSummaryPartDone {
		return nil, fmt.Errorf("responses: SummaryPart called on Event.Type=%q", e.Type)
	}
	if len(e.Part) == 0 {
		return nil, errors.New("responses: empty Part on reasoning_summary_part event")
	}
	var out SummaryPart
	if err := json.Unmarshal(e.Part, &out); err != nil {
		return nil, fmt.Errorf("responses: decode SummaryPart: %w", err)
	}
	return &out, nil
}

// APIError decodes Error as an APIError. Only valid on error events;
// returns an error if Type is incompatible or Error is empty.
func (e *Event) APIError() (*APIError, error) {
	if e == nil {
		return nil, errors.New("responses: nil event")
	}
	if e.Type != EventTypeError {
		return nil, fmt.Errorf("responses: APIError called on Event.Type=%q", e.Type)
	}
	if len(e.Error) == 0 {
		return nil, errors.New("responses: empty Error on error event")
	}
	var out APIError
	if err := json.Unmarshal(e.Error, &out); err != nil {
		return nil, fmt.Errorf("responses: decode APIError: %w", err)
	}
	return &out, nil
}

// Stream consumes a Responses Server-Sent-Events response body,
// decoding each frame into an Event. The caller iterates by calling
// Recv until io.EOF.
//
// Stream is NOT safe for concurrent use; one goroutine drives a
// stream from start to finish, then calls Close.
type Stream struct {
	rc      io.ReadCloser
	scanner *bufio.Scanner
	done    bool
	err     error
}

// newStream wraps an io.ReadCloser in a Stream with an SSE-aware
// scanner. maxFrame caps the per-frame buffer; 0 falls back to
// DefaultMaxFrameSize.
func newStream(rc io.ReadCloser, maxFrame int) *Stream {
	if maxFrame <= 0 {
		maxFrame = DefaultMaxFrameSize
	}
	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, 64*1024), maxFrame)
	scanner.Split(splitSSEFrames)
	return &Stream{rc: rc, scanner: scanner}
}

// Recv returns the next decoded event. Returns io.EOF when the
// underlying body has ended or a terminal lifecycle event
// (response.completed / failed / cancelled / incomplete) has been
// emitted and consumed. Subsequent calls after EOF continue to
// return io.EOF.
//
// Note: the Responses API does NOT emit a [DONE] sentinel —
// terminal lifecycle events ARE the end signal. The stream body
// closes shortly after; the underlying io.EOF lands on the next
// Recv. Recv does not auto-terminate on lifecycle events so the
// caller sees the terminal Event.
func (s *Stream) Recv() (*Event, error) {
	if s.done {
		if s.err != nil {
			return nil, s.err
		}
		return nil, io.EOF
	}

	for s.scanner.Scan() {
		frame := s.scanner.Bytes()
		payload, ok := extractDataPayload(frame)
		if !ok {
			continue
		}
		ev := &Event{}
		if err := json.Unmarshal(payload, ev); err != nil {
			s.done = true
			s.err = fmt.Errorf("responses: malformed stream event: %w", err)
			return nil, s.err
		}
		return ev, nil
	}

	if err := s.scanner.Err(); err != nil {
		s.done = true
		s.err = fmt.Errorf("responses: stream read: %w", err)
		return nil, s.err
	}

	s.done = true
	return nil, io.EOF
}

// Close releases the underlying response body. Safe to call multiple
// times.
func (s *Stream) Close() error {
	if s.rc == nil {
		return nil
	}
	err := s.rc.Close()
	s.rc = nil
	return err
}

// extractDataPayload pulls the JSON body out of an SSE frame. Frames
// contain `event: <name>` and `data: <json>` lines (and optionally
// `id:`, `retry:`); we ignore the event line — the typed data
// payload's "type" field is authoritative — and return the data
// body. Multiple data lines in one frame are joined with "\n" per
// the SSE spec.
func extractDataPayload(frame []byte) (payload []byte, ok bool) {
	var out bytes.Buffer
	found := false
	for _, raw := range bytes.Split(frame, []byte("\n")) {
		line := bytes.TrimRight(raw, "\r")
		const prefix = "data:"
		if !bytes.HasPrefix(line, []byte(prefix)) {
			continue
		}
		body := bytes.TrimSpace(line[len(prefix):])
		if found {
			out.WriteByte('\n')
		}
		out.Write(body)
		found = true
	}
	if !found {
		return nil, false
	}
	return out.Bytes(), true
}

// splitSSEFrames is a bufio.SplitFunc that returns one SSE frame
// per call. Frames are separated by blank lines: \n\n or \r\n\r\n.
// Identical to the wire-package implementation; duplicated here to
// keep the responses package self-contained (no cross-package
// internal coupling).
func splitSSEFrames(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.Index(data, []byte("\n\n")); i >= 0 {
		return i + 2, data[:i], nil
	}
	if i := bytes.Index(data, []byte("\r\n\r\n")); i >= 0 {
		return i + 4, data[:i], nil
	}
	if atEOF {
		return len(data), bytes.TrimRight(data, "\r\n"), nil
	}
	return 0, nil, nil
}
