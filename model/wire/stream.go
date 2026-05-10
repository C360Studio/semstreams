package wire

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// DefaultMaxFrameSize is the default per-SSE-frame buffer ceiling. A
// single tool_call argument blob can exceed 1 MiB on long-arg flows;
// 16 MiB gives substantial headroom while still protecting against
// runaway provider output. Configurable via ClientConfig.MaxFrameSize.
const DefaultMaxFrameSize = 16 * 1024 * 1024

// streamDoneSentinel is the SSE payload that signals end-of-stream
// for OpenAI-compat ChatCompletion. Providers consistent with the
// OpenAI shape emit "data: [DONE]" before closing the body.
const streamDoneSentinel = "[DONE]"

// Stream consumes a Server-Sent-Events response body, decoding each
// `data:` frame into a StreamChunk. The caller iterates by calling
// Recv until io.EOF.
//
// Stream is NOT safe for concurrent use; one goroutine drives a stream
// from start to finish, then calls Close.
type Stream struct {
	rc      io.ReadCloser
	scanner *bufio.Scanner
	done    bool
	err     error
}

// newStream wraps an io.ReadCloser in a Stream with an SSE-aware
// scanner. The scanner uses a custom split function that breaks on
// blank-line frame separators (\n\n or \r\n\r\n). maxFrame caps the
// per-frame buffer; 0 falls back to DefaultMaxFrameSize.
func newStream(rc io.ReadCloser, maxFrame int) *Stream {
	if maxFrame <= 0 {
		maxFrame = DefaultMaxFrameSize
	}
	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, 64*1024), maxFrame)
	scanner.Split(splitSSEFrames)
	return &Stream{rc: rc, scanner: scanner}
}

// Recv returns the next decoded chunk. Returns io.EOF when the stream
// has emitted the [DONE] sentinel or the underlying body has ended.
// Subsequent calls after EOF continue to return io.EOF.
func (s *Stream) Recv() (*StreamChunk, error) {
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
		if string(payload) == streamDoneSentinel {
			s.done = true
			return nil, io.EOF
		}

		chunk := &StreamChunk{}
		if err := json.Unmarshal(payload, chunk); err != nil {
			s.done = true
			s.err = fmt.Errorf("wire: malformed stream chunk: %w", err)
			return nil, s.err
		}
		return chunk, nil
	}

	if err := s.scanner.Err(); err != nil {
		s.done = true
		s.err = fmt.Errorf("wire: stream read: %w", err)
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
// are line-oriented; each line starting with "data:" contributes its
// remainder (after the colon-space) to the payload. Multiple data
// lines in one frame are joined with "\n" per the SSE spec, though
// ChatCompletion providers typically emit single-line frames.
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

// splitSSEFrames is a bufio.SplitFunc that returns one SSE frame per
// call. Frames are separated by blank lines: \n\n or \r\n\r\n. The
// returned token excludes the separator.
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

// Accumulator merges streaming deltas into a final Message. The merge
// rules implement ADR-037 §2b:
//
//   - Last-writer-wins on Extras key collisions, with a debug log line
//     emitted via OnConflict if set.
//   - No deep-merge on Extras: a later chunk replaces the prior value
//     wholesale.
//   - Per-tool_call merging keyed by Index; each ToolCall accumulates
//     Function.Name, Function.Arguments (string concat), and Extras
//     (last-writer-wins).
//
// Use NewAccumulator + Add per chunk + Final at end-of-stream. The
// zero value is invalid; always go through NewAccumulator.
type Accumulator struct {
	role             string
	content          strings.Builder
	hasContent       bool
	reasoningContent strings.Builder
	hasReasoning     bool
	toolCallByIndex  map[int]*ToolCall
	toolCallOrder    []int
	messageExtras    map[string]json.RawMessage
	finishReason     string
	usage            *Usage

	// OnConflict is called for each Extras key overwrite during
	// streaming. Useful for surfacing silent-overwrite bugs without
	// failing the stream. nil disables.
	OnConflict func(scope, key string)
}

// NewAccumulator constructs a fresh Accumulator.
func NewAccumulator() *Accumulator {
	return &Accumulator{
		toolCallByIndex: make(map[int]*ToolCall),
	}
}

// Add merges one StreamChunk into the accumulator. choices[0] is the
// only choice consumed; multi-choice streaming is rare and not
// supported in the v1 wire layer (callers wanting n>1 should use
// non-streaming completions).
func (a *Accumulator) Add(chunk *StreamChunk) error {
	if chunk == nil {
		return errors.New("wire: nil chunk")
	}
	if chunk.Usage != nil {
		a.usage = chunk.Usage
	}
	if len(chunk.Choices) == 0 {
		return nil
	}
	c := chunk.Choices[0]
	if c.FinishReason != "" {
		a.finishReason = c.FinishReason
	}
	if c.Delta == nil {
		return nil
	}
	d := c.Delta

	if d.Role != "" && a.role == "" {
		a.role = d.Role
	}
	if s, ok := d.ContentString(); ok && s != "" {
		a.content.WriteString(s)
		a.hasContent = true
	}
	if d.ReasoningContent != "" {
		a.reasoningContent.WriteString(d.ReasoningContent)
		a.hasReasoning = true
	}

	for _, tc := range d.ToolCalls {
		a.mergeToolCall(tc)
	}

	a.mergeExtras("message", &a.messageExtras, d.Extras)
	return nil
}

// mergeToolCall folds one delta tool_call element into the indexed
// accumulator. Index resolution:
//
//   - delta.Index set: keys directly by that index.
//   - delta.Index nil + delta.ID set + matches an in-flight call's ID:
//     merges into that call (some providers omit index on continuation
//     fragments).
//   - delta.Index nil + delta.ID matches no in-flight call: starts a
//     new slot at the next free index.
func (a *Accumulator) mergeToolCall(delta ToolCall) {
	var idx int
	switch {
	case delta.Index != nil:
		idx = *delta.Index
	case delta.ID != "":
		idx = a.indexOfToolCallByID(delta.ID)
		if idx < 0 {
			idx = len(a.toolCallOrder)
		}
	default:
		// Index AND ID missing: continuation of the most-recent
		// in-flight call (typical of providers that emit index only on
		// the opening frame).
		if len(a.toolCallOrder) > 0 {
			idx = a.toolCallOrder[len(a.toolCallOrder)-1]
		} else {
			idx = 0
		}
	}
	cur, ok := a.toolCallByIndex[idx]
	if !ok {
		cur = &ToolCall{Index: intPtr(idx)}
		a.toolCallByIndex[idx] = cur
		a.toolCallOrder = append(a.toolCallOrder, idx)
	}
	if delta.ID != "" {
		cur.ID = delta.ID
	}
	if delta.Type != "" {
		cur.Type = delta.Type
	}
	if delta.Function.Name != "" {
		cur.Function.Name = delta.Function.Name
	}
	if delta.Function.Arguments != "" {
		cur.Function.Arguments += delta.Function.Arguments
	}
	a.mergeExtras(fmt.Sprintf("tool_call[%d]", idx), &cur.Extras, delta.Extras)
	a.mergeExtras(fmt.Sprintf("tool_call[%d].function", idx), &cur.Function.Extras, delta.Function.Extras)
}

// mergeExtras applies the §2b last-writer-wins rule. Calls OnConflict
// for every overwrite where the new bytes differ from the prior bytes
// (idempotent overwrites — providers that re-send identical extras on
// every delta of the same logical tool_call — are silent).
func (a *Accumulator) mergeExtras(scope string, dst *map[string]json.RawMessage, src map[string]json.RawMessage) {
	if len(src) == 0 {
		return
	}
	if *dst == nil {
		*dst = make(map[string]json.RawMessage, len(src))
	}
	for k, v := range src {
		if prev, exists := (*dst)[k]; exists && a.OnConflict != nil && !bytes.Equal(prev, v) {
			a.OnConflict(scope, k)
		}
		(*dst)[k] = v
	}
}

// indexOfToolCallByID returns the index of an in-flight tool_call by
// ID, or -1 if no match.
func (a *Accumulator) indexOfToolCallByID(id string) int {
	for _, idx := range a.toolCallOrder {
		if a.toolCallByIndex[idx].ID == id {
			return idx
		}
	}
	return -1
}

// Final returns the accumulated Message, finish reason, and usage.
// finishReason is empty if no chunk supplied one. usage is nil if no
// chunk reported it.
func (a *Accumulator) Final() (msg Message, finishReason string, usage *Usage) {
	msg = Message{Role: a.role}
	if a.hasContent {
		_ = msg.SetContentString(a.content.String())
	}
	if a.hasReasoning {
		msg.ReasoningContent = a.reasoningContent.String()
	}
	if len(a.toolCallOrder) > 0 {
		msg.ToolCalls = make([]ToolCall, 0, len(a.toolCallOrder))
		for _, idx := range a.toolCallOrder {
			msg.ToolCalls = append(msg.ToolCalls, *a.toolCallByIndex[idx])
		}
	}
	if len(a.messageExtras) > 0 {
		msg.Extras = a.messageExtras
	}
	return msg, a.finishReason, a.usage
}

func intPtr(v int) *int { return &v }
