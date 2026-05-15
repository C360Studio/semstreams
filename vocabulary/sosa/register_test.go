package sosa

import "testing"

// TestRegisterIsIdempotent confirms calling Register again after
// the init-time call is a no-op rather than a collision. This
// protects callers that defensively wire the prefix table from
// scratch without skipping our init.
//
// End-to-end integration with vocabulary/export's prefix
// compaction is covered by the Phase 2 smoke test in
// vocabulary/export/phase2_smoke_test.go.
func TestRegisterIsIdempotent(t *testing.T) {
	if err := Register(); err != nil {
		t.Fatalf("Register() after init: %v", err)
	}
	if err := Register(); err != nil {
		t.Fatalf("Register() second call: %v", err)
	}
}
