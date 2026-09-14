package agentic_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
)

// TestLoopEntity_DecodesRecordsCarryingRemovedPauseKeys guards the deleted
// fields without granting retired state values compatibility acceptance.
func TestLoopEntity_DecodesRecordsCarryingRemovedPauseKeys(t *testing.T) {
	t.Parallel()

	// Unknown JSON fields remain ignored; re-adding a removed field would
	// capture these values and emit them on the round-trip.
	input := []byte(`{
		"id": "loop-001",
		"state": "running",
		"max_iterations": 1,
		"pause_requested": true,
		"pause_requested_by": "user-7",
		"state_before_pause": "planning"
	}`)

	var got agentic.LoopEntity
	if err := json.Unmarshal(input, &got); err != nil {
		t.Fatalf("record with unknown removed keys must decode: %v", err)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	if got.ID != "loop-001" {
		t.Errorf("ID = %q, want loop-001", got.ID)
	}
	if got.State != agentic.LoopStateRunning {
		t.Errorf("State = %q, want running", got.State)
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
