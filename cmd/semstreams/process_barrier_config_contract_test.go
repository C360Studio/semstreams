package main

import (
	"os"
	"strings"
	"testing"
)

// TestShippedAgenticConfigDoesNotAdmitProcessBarrier keeps the E2E-only barrier
// executor out of the config this repository ships. The build-time half — that
// only the tagged target compiles it, and only into the binary the agentic tier
// boots — is pinned by test/contract/e2e_tier_binary_contract_test.go.
func TestShippedAgenticConfigDoesNotAdmitProcessBarrier(t *testing.T) {
	shipped, err := os.ReadFile("../../configs/agentic.json")
	if err != nil {
		t.Fatalf("read shipped agentic config: %v", err)
	}
	if strings.Contains(string(shipped), "e2e_process_barrier") {
		t.Fatal("shipped agentic config admits the E2E-only process barrier")
	}
}
