package query

import (
	"testing"
)

// TestDefaultConfig_NoLifecycleRetentionOnGraphBuckets locks the ADR-068 D1
// guardrail at the source of the former landmine: the query client's default
// config must set NO TTL on the shared live graph buckets (a TTL here can win
// the get-or-create race and silently expire ENTITY_STATES).
func TestDefaultConfig_NoLifecycleRetentionOnGraphBuckets(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.EntityStates.TTL != 0 {
		t.Errorf("ENTITY_STATES TTL = %v, want 0 (ADR-068 D1: no lifecycle retention on the live graph)", cfg.EntityStates.TTL)
	}
	if cfg.SpatialIndex.TTL != 0 {
		t.Errorf("SPATIAL_INDEX TTL = %v, want 0 (ADR-068 D1)", cfg.SpatialIndex.TTL)
	}
	if cfg.IncomingIndex.TTL != 0 {
		t.Errorf("INCOMING_INDEX TTL = %v, want 0 (ADR-068 D1)", cfg.IncomingIndex.TTL)
	}
}
