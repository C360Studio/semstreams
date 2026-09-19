package agentic_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
)

// TestLoopEntity_DecodesRecordsCarryingRemovedPauseKeys pins the compatibility
// claim made in docs/operations/migration-beta162-to-beta163.md: #1239 deleted
// PauseRequested / PauseRequestedBy / StateBeforePause, and an AGENT_LOOPS record
// written before that deletion still carries pause_requested, pause_requested_by
// and state_before_pause. Those records must still load, or the deletion is a
// silent data-loss migration rather than the removal of a dead surface.
func TestLoopEntity_DecodesRecordsCarryingRemovedPauseKeys(t *testing.T) {
	t.Parallel()

	// A record as it was persisted before the removal, with the three keys set
	// to non-zero values — a re-added field would capture them and show up in
	// the re-marshal below, so this also guards against quietly reintroducing
	// the surface.
	legacy := []byte(`{
		"id": "loop-legacy-001",
		"state": "executing",
		"pause_requested": true,
		"pause_requested_by": "user-7",
		"state_before_pause": "planning"
	}`)

	var got agentic.LoopEntity
	if err := json.Unmarshal(legacy, &got); err != nil {
		t.Fatalf("legacy record with removed pause keys must still decode: %v", err)
	}

	if got.ID != "loop-legacy-001" {
		t.Errorf("ID = %q, want loop-legacy-001", got.ID)
	}
	if got.State != agentic.LoopStateExecuting {
		t.Errorf("State = %q, want executing", got.State)
	}

	// The keys must not survive a round-trip: they are gone from the type, so
	// re-marshalling the decoded record drops them.
	round, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{"pause_requested", "pause_requested_by", "state_before_pause"} {
		if bytes.Contains(round, []byte(key)) {
			t.Errorf("removed key %q reappeared on re-marshal: %s", key, round)
		}
	}
}
