package e2eboot

import (
	"os"
	"strings"
	"testing"
)

// TestShippedAgenticConfigDoesNotAdmitProcessBarrier keeps the E2E-only barrier
// executor out of the config this repository ships; the process-barrier option
// admits it at boot instead. The link-time half — that only the E2E binary links
// the barrier, and only the agentic tier enables it — is pinned by
// test/contract/e2e_tier_binary_contract_test.go.
func TestShippedAgenticConfigDoesNotAdmitProcessBarrier(t *testing.T) {
	shipped, err := os.ReadFile("../../configs/agentic.json")
	if err != nil {
		t.Fatalf("read shipped agentic config: %v", err)
	}
	if strings.Contains(string(shipped), "e2e_process_barrier") {
		t.Fatal("shipped agentic config admits the E2E-only process barrier")
	}
}
