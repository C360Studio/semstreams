package responses

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Accumulator builds a Response from a Responses event stream.
//
// Per the API contract, response.completed (and the other terminal
// lifecycle events) carry the full final Response object. The
// accumulator promotes that to the terminal state when seen and
// Final returns it directly. As a fallback for streams that
// terminate without a lifecycle event (server-side cut, body
// truncation, etc.), the accumulator builds an incremental Response
// from the per-item / per-delta events so partial-stream callers
// still get something usable.
//
// Accumulator is NOT safe for concurrent use; one goroutine drives
// one stream.
type Accumulator struct {
	final *Response

	// incremental state — used when no terminal lifecycle event
	// arrives. Items are keyed by output_index; delta accumulation
	// happens on the slot's content/summary/arguments fields.
	items map[int]*OutputItem
}

// NewAccumulator constructs a fresh Accumulator.
func NewAccumulator() *Accumulator {
	return &Accumulator{
		items: make(map[int]*OutputItem),
	}
}

// Add merges one Event into the accumulator. Errors are returned
// for malformed events (e.g. delta with no output_index) so callers
// can decide whether to halt the stream or skip; the recommended
// behavior for the agentic-model adapter layer is to log + skip
// since the Responses API itself promises well-formed events under
// normal conditions.
func (a *Accumulator) Add(ev *Event) error {
	if ev == nil {
		return errors.New("responses: nil event")
	}
	switch ev.Type {
	case EventTypeResponseCompleted,
		EventTypeResponseFailed,
		EventTypeResponseCancelled,
		EventTypeResponseIncomplete:
		// Terminal lifecycle: snapshot the full response.
		if ev.Response != nil {
			a.final = ev.Response
		}
		return nil

	case EventTypeResponseCreated, EventTypeResponseInProgress:
		// Pre-terminal lifecycle: carry forward the response shell so
		// metadata (id, model, status) is populated even if no
		// terminal event arrives.
		if ev.Response != nil && a.final == nil {
			a.final = ev.Response
		}
		return nil

	case EventTypeOutputItemAdded:
		return a.handleOutputItemAdded(ev)

	case EventTypeOutputItemDone:
		return a.handleOutputItemDone(ev)

	case EventTypeContentPartAdded:
		return a.handleContentPartAdded(ev)

	case EventTypeContentPartDone:
		return a.handleContentPartDone(ev)

	case EventTypeOutputTextDelta:
		return a.handleTextDelta(ev, ev.Delta, false)

	case EventTypeOutputTextDone:
		return a.handleTextDone(ev, ev.Text, false)

	case EventTypeRefusalDelta:
		return a.handleTextDelta(ev, ev.Delta, true)

	case EventTypeRefusalDone:
		return a.handleTextDone(ev, ev.Refusal, true)

	case EventTypeFunctionCallArgumentsDelta:
		return a.handleArgsDelta(ev)

	case EventTypeFunctionCallArgumentsDone:
		return a.handleArgsDone(ev)

	case EventTypeReasoningSummaryPartAdded:
		return a.handleSummaryPartAdded(ev)

	case EventTypeReasoningSummaryPartDone:
		return a.handleSummaryPartDone(ev)

	case EventTypeReasoningSummaryTextDelta:
		return a.handleSummaryTextDelta(ev)

	case EventTypeReasoningSummaryTextDone:
		return a.handleSummaryTextDone(ev)

	case EventTypeError:
		// Errors aren't merged into Response state; callers detect
		// them via the Event and decide how to surface. The stream
		// typically follows with a terminal failed event anyway.
		return nil

	default:
		// Forward-compat: unknown event types are ignored so a
		// future API extension doesn't break old clients.
		return nil
	}
}

// Final returns the accumulated Response. Prefers the terminal
// lifecycle snapshot when present; falls back to the incrementally-
// assembled Response built from item/delta events. Returns nil if
// no relevant events were seen.
func (a *Accumulator) Final() *Response {
	if a.final != nil {
		// If we have items accumulated incrementally that the
		// lifecycle snapshot did not carry (truncated server-side
		// final), merge them in. Common case: final.Output is
		// fully-populated and items is a subset — preferring final.
		if len(a.final.Output) == 0 && len(a.items) > 0 {
			a.final.Output = a.snapshotItems()
		}
		return a.final
	}
	if len(a.items) == 0 {
		return nil
	}
	return &Response{
		Status: "incomplete",
		Output: a.snapshotItems(),
	}
}

// snapshotItems materializes the incremental items map into an
// output_index-sorted slice. Stable order matters for replay /
// trace consumers.
func (a *Accumulator) snapshotItems() []OutputItem {
	indices := make([]int, 0, len(a.items))
	for idx := range a.items {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	out := make([]OutputItem, len(indices))
	for i, idx := range indices {
		out[i] = *a.items[idx]
	}
	return out
}

// requireOutputIndex returns *ev.OutputIndex or an error. Centralizes
// the nil-check across many handlers.
func requireOutputIndex(ev *Event, eventType string) (int, error) {
	if ev.OutputIndex == nil {
		return 0, fmt.Errorf("responses: %s missing output_index", eventType)
	}
	return *ev.OutputIndex, nil
}

// itemAt returns the OutputItem at output_index, creating a stub
// keyed by item_id if absent. The stub is enough to absorb delta
// events that arrive before the output_item.added carrier.
func (a *Accumulator) itemAt(idx int, itemID string) *OutputItem {
	if cur, ok := a.items[idx]; ok {
		return cur
	}
	cur := &OutputItem{ID: itemID}
	a.items[idx] = cur
	return cur
}

func (a *Accumulator) handleOutputItemAdded(ev *Event) error {
	idx, err := requireOutputIndex(ev, "output_item.added")
	if err != nil {
		return err
	}
	if ev.Item == nil {
		return errors.New("responses: output_item.added missing item")
	}
	item := *ev.Item
	a.items[idx] = &item
	return nil
}

func (a *Accumulator) handleOutputItemDone(ev *Event) error {
	idx, err := requireOutputIndex(ev, "output_item.done")
	if err != nil {
		return err
	}
	if ev.Item == nil {
		return errors.New("responses: output_item.done missing item")
	}
	item := *ev.Item
	a.items[idx] = &item
	return nil
}

func (a *Accumulator) handleContentPartAdded(ev *Event) error {
	idx, err := requireOutputIndex(ev, "content_part.added")
	if err != nil {
		return err
	}
	cur := a.itemAt(idx, ev.ItemID)
	part, err := ev.ContentPart()
	if err != nil {
		return err
	}
	cur.Content = appendAtIndex(cur.Content, ev.ContentIndex, *part)
	return nil
}

func (a *Accumulator) handleContentPartDone(ev *Event) error {
	idx, err := requireOutputIndex(ev, "content_part.done")
	if err != nil {
		return err
	}
	cur := a.itemAt(idx, ev.ItemID)
	part, err := ev.ContentPart()
	if err != nil {
		return err
	}
	cur.Content = setAtIndex(cur.Content, ev.ContentIndex, *part)
	return nil
}

// handleTextDelta merges a partial text/refusal delta into the
// addressed content part. refusal=true routes to ContentPart.Refusal;
// otherwise to .Text.
func (a *Accumulator) handleTextDelta(ev *Event, delta string, refusal bool) error {
	idx, err := requireOutputIndex(ev, "text/refusal.delta")
	if err != nil {
		return err
	}
	cur := a.itemAt(idx, ev.ItemID)
	ci := indexOrZero(ev.ContentIndex)
	cur.Content = ensureSize(cur.Content, ci+1)
	if refusal {
		cur.Content[ci].Type = ContentTypeRefusal
		cur.Content[ci].Refusal += delta
	} else {
		cur.Content[ci].Type = ContentTypeOutputText
		cur.Content[ci].Text += delta
	}
	return nil
}

// handleTextDone snapshots the final text on the addressed content
// part. The done text replaces (not appends to) accumulated deltas
// per the API contract.
func (a *Accumulator) handleTextDone(ev *Event, text string, refusal bool) error {
	idx, err := requireOutputIndex(ev, "text/refusal.done")
	if err != nil {
		return err
	}
	cur := a.itemAt(idx, ev.ItemID)
	ci := indexOrZero(ev.ContentIndex)
	cur.Content = ensureSize(cur.Content, ci+1)
	if refusal {
		cur.Content[ci].Type = ContentTypeRefusal
		cur.Content[ci].Refusal = text
	} else {
		cur.Content[ci].Type = ContentTypeOutputText
		cur.Content[ci].Text = text
	}
	return nil
}

func (a *Accumulator) handleArgsDelta(ev *Event) error {
	idx, err := requireOutputIndex(ev, "function_call_arguments.delta")
	if err != nil {
		return err
	}
	cur := a.itemAt(idx, ev.ItemID)
	cur.Type = ItemTypeFunctionCall
	var b strings.Builder
	b.WriteString(cur.Arguments)
	b.WriteString(ev.Delta)
	cur.Arguments = b.String()
	return nil
}

func (a *Accumulator) handleArgsDone(ev *Event) error {
	idx, err := requireOutputIndex(ev, "function_call_arguments.done")
	if err != nil {
		return err
	}
	cur := a.itemAt(idx, ev.ItemID)
	cur.Type = ItemTypeFunctionCall
	cur.Arguments = ev.Arguments
	return nil
}

func (a *Accumulator) handleSummaryPartAdded(ev *Event) error {
	idx, err := requireOutputIndex(ev, "reasoning_summary_part.added")
	if err != nil {
		return err
	}
	cur := a.itemAt(idx, ev.ItemID)
	cur.Type = ItemTypeReasoning
	part, err := ev.SummaryPart()
	if err != nil {
		return err
	}
	cur.Summary = appendSummaryAtIndex(cur.Summary, ev.SummaryIndex, *part)
	return nil
}

func (a *Accumulator) handleSummaryPartDone(ev *Event) error {
	idx, err := requireOutputIndex(ev, "reasoning_summary_part.done")
	if err != nil {
		return err
	}
	cur := a.itemAt(idx, ev.ItemID)
	cur.Type = ItemTypeReasoning
	part, err := ev.SummaryPart()
	if err != nil {
		return err
	}
	cur.Summary = setSummaryAtIndex(cur.Summary, ev.SummaryIndex, *part)
	return nil
}

func (a *Accumulator) handleSummaryTextDelta(ev *Event) error {
	idx, err := requireOutputIndex(ev, "reasoning_summary_text.delta")
	if err != nil {
		return err
	}
	cur := a.itemAt(idx, ev.ItemID)
	cur.Type = ItemTypeReasoning
	si := indexOrZero(ev.SummaryIndex)
	cur.Summary = ensureSummarySize(cur.Summary, si+1)
	cur.Summary[si].Type = SummaryTypeText
	cur.Summary[si].Text += ev.Delta
	return nil
}

func (a *Accumulator) handleSummaryTextDone(ev *Event) error {
	idx, err := requireOutputIndex(ev, "reasoning_summary_text.done")
	if err != nil {
		return err
	}
	cur := a.itemAt(idx, ev.ItemID)
	cur.Type = ItemTypeReasoning
	si := indexOrZero(ev.SummaryIndex)
	cur.Summary = ensureSummarySize(cur.Summary, si+1)
	cur.Summary[si].Type = SummaryTypeText
	cur.Summary[si].Text = ev.Text
	return nil
}

// indexOrZero returns *p or 0 when p is nil. Used for delta events
// where the content_index/summary_index is technically optional but
// the slot defaults to 0 in practice.
func indexOrZero(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// ensureSize grows the content slice to at least n entries, filling
// new slots with the zero-value ContentPart so direct indexing is
// safe.
func ensureSize(parts []ContentPart, n int) []ContentPart {
	for len(parts) < n {
		parts = append(parts, ContentPart{})
	}
	return parts
}

// ensureSummarySize is ensureSize for the SummaryPart slice.
func ensureSummarySize(parts []SummaryPart, n int) []SummaryPart {
	for len(parts) < n {
		parts = append(parts, SummaryPart{})
	}
	return parts
}

// appendAtIndex sets parts[idx] = p, growing the slice as needed.
// When idx is nil the part is appended at len(parts).
func appendAtIndex(parts []ContentPart, idx *int, p ContentPart) []ContentPart {
	i := indexOrZero(idx)
	if idx == nil {
		i = len(parts)
	}
	parts = ensureSize(parts, i+1)
	parts[i] = p
	return parts
}

// setAtIndex replaces parts[idx] with p, growing the slice as needed.
func setAtIndex(parts []ContentPart, idx *int, p ContentPart) []ContentPart {
	i := indexOrZero(idx)
	parts = ensureSize(parts, i+1)
	parts[i] = p
	return parts
}

// appendSummaryAtIndex is appendAtIndex for the SummaryPart slice.
func appendSummaryAtIndex(parts []SummaryPart, idx *int, p SummaryPart) []SummaryPart {
	i := indexOrZero(idx)
	if idx == nil {
		i = len(parts)
	}
	parts = ensureSummarySize(parts, i+1)
	parts[i] = p
	return parts
}

// setSummaryAtIndex is setAtIndex for the SummaryPart slice.
func setSummaryAtIndex(parts []SummaryPart, idx *int, p SummaryPart) []SummaryPart {
	i := indexOrZero(idx)
	parts = ensureSummarySize(parts, i+1)
	parts[i] = p
	return parts
}
