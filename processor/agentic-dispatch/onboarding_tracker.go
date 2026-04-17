package agenticdispatch

import (
	"maps"

	operatingmodel "github.com/c360studio/semstreams/agentic/operating-model"
)

// snapshotLoop returns a deep copy of the LoopInfo for loopID suitable for
// unlocked reads. The returned pointer is safe to traverse even while the
// tracker mutates the underlying entry from another goroutine — the interview
// handler uses this to avoid racing with /cancel, sibling turns, or any
// other writer touching the loop.
func snapshotLoop(t *LoopTracker, loopID string) *LoopInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()
	info, ok := t.loops[loopID]
	if !ok {
		return nil
	}
	clone := *info
	if info.Metadata != nil {
		clone.Metadata = maps.Clone(info.Metadata)
	}
	return &clone
}

// onboardSubState returns the onboarding sub-state from a loop's Metadata,
// defaulting to SubStateAwaitingAnswer when the key is absent (the state a
// freshly-minted onboarding loop starts in).
func onboardSubState(info *LoopInfo) string {
	if info == nil {
		return SubStateAwaitingAnswer
	}
	if s := metaString(info.Metadata, OnboardMetaSubState); s != "" {
		return s
	}
	return SubStateAwaitingAnswer
}

// metaString reads a string value from Metadata with a safe default.
func metaString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	v, ok := meta[key].(string)
	if !ok {
		return ""
	}
	return v
}

// metaInt reads an int value from Metadata with a safe default. Tolerates
// float64 which is what a JSON-round-tripped map produces.
func metaInt(meta map[string]any, key string, def int) int {
	if meta == nil {
		return def
	}
	switch v := meta[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return def
}

// draftEntriesFromMetadata extracts the draft entries stored under
// OnboardMetaDraftEntries. Accepts either []operatingmodel.Entry (in-memory
// path) or []any (JSON-round-tripped path, not currently exercised but
// future-proofed for persisted trackers).
func draftEntriesFromMetadata(meta map[string]any) ([]operatingmodel.Entry, bool) {
	if meta == nil {
		return nil, false
	}
	switch v := meta[OnboardMetaDraftEntries].(type) {
	case []operatingmodel.Entry:
		return v, true
	case []any:
		out := make([]operatingmodel.Entry, 0, len(v))
		for _, raw := range v {
			m, ok := raw.(map[string]any)
			if !ok {
				return nil, false
			}
			entry := operatingmodel.Entry{
				EntryID:          metaString(m, "entry_id"),
				Title:            metaString(m, "title"),
				Summary:          metaString(m, "summary"),
				Cadence:          metaString(m, "cadence"),
				Trigger:          metaString(m, "trigger"),
				SourceConfidence: metaString(m, "source_confidence"),
				Status:           metaString(m, "status"),
			}
			out = append(out, entry)
		}
		return out, len(out) > 0
	}
	return nil, false
}

// updateMetadata merges the given keys into the loop's Metadata map, creating
// the map if needed. Uses the tracker's mutex for safety.
func updateMetadata(t *LoopTracker, loopID string, updates map[string]any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	info, ok := t.loops[loopID]
	if !ok {
		return
	}
	if info.Metadata == nil {
		info.Metadata = make(map[string]any, len(updates))
	}
	for k, v := range updates {
		info.Metadata[k] = v
	}
}

// advanceToNextLayer records the move to a new interview layer. Resets
// sub-state to awaiting_answer and clears the previous draft so the new
// layer starts with a clean slate.
//
// expectedLayer guards against skipping ahead when a concurrent mutation
// (e.g. /cancel or a duplicate approval on restart) has already moved the
// loop past the layer we thought we were advancing from. Returns true iff
// the mutation was applied.
func advanceToNextLayer(t *LoopTracker, loopID, expectedLayer, nextLayer string, layerOrder int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	info, ok := t.loops[loopID]
	if !ok || info.WorkflowStep != expectedLayer {
		return false
	}
	info.WorkflowStep = nextLayer
	if info.Metadata == nil {
		info.Metadata = make(map[string]any, 3)
	}
	info.Metadata[OnboardMetaSubState] = SubStateAwaitingAnswer
	info.Metadata[OnboardMetaLayerOrder] = layerOrder
	delete(info.Metadata, OnboardMetaDraftEntries)
	delete(info.Metadata, OnboardMetaDraftSummary)
	return true
}

// finalizeOnboardingLoop marks the loop as successfully completed and clears
// transient draft fields. Existing /status and /loops commands recognize the
// "complete" terminal state without any special-casing.
//
// expectedLayer guards against finalizing when a concurrent mutation has
// already moved the loop elsewhere. Returns true iff the mutation was applied.
func finalizeOnboardingLoop(t *LoopTracker, loopID, expectedLayer string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	info, ok := t.loops[loopID]
	if !ok || info.WorkflowStep != expectedLayer {
		return false
	}
	info.State = "complete"
	info.Outcome = "success"
	if info.Metadata != nil {
		delete(info.Metadata, OnboardMetaDraftEntries)
		delete(info.Metadata, OnboardMetaDraftSummary)
		delete(info.Metadata, OnboardMetaSubState)
	}
	return true
}

// recordLayerApproved increments the loop's completed-layer counter. Exactly
// one call per successful LayerApproved publish — advance/finalize no longer
// mutate Iterations, so /status accurately reflects how many layers the user
// has saved.
func recordLayerApproved(t *LoopTracker, loopID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if info, ok := t.loops[loopID]; ok {
		info.Iterations++
	}
}
