package swe

import "testing"

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
