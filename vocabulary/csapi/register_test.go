package csapi

import "testing"

// End-to-end integration with vocabulary/export's prefix compaction
// is covered by the smoke test suite that iterates over registered
// prefixes; this test guards the package-local idempotency contract.
func TestRegisterIsIdempotent(t *testing.T) {
	if err := Register(); err != nil {
		t.Fatalf("Register() after init: %v", err)
	}
	if err := Register(); err != nil {
		t.Fatalf("Register() second call: %v", err)
	}
}
