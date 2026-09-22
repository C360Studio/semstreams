package ops

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestE2ECompositionRootLoadsCheckedInPersonaFragments pins the one thing the
// ops tier needs from the E2E composition root beyond its lesson-curation
// control responder: the persona fragments must be loaded before the ops loop
// starts. That the ops tier boots that root at all — target e2e, binary
// cmd/e2e-semstreams, its own image tag — is pinned for every tier at once by
// test/contract/e2e_tier_binary_contract_test.go.
func TestE2ECompositionRootLoadsCheckedInPersonaFragments(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "../../../.."))

	e2eMain, err := os.ReadFile(filepath.Join(root, "cmd/e2e-semstreams/main.go"))
	if err != nil {
		t.Fatalf("read E2E composition root: %v", err)
	}
	if !strings.Contains(string(e2eMain), `persona.LoadFromDirectory(ctx, "configs/personas/fragments", personaMgr, logger)`) {
		t.Error("E2E composition root must load checked-in persona fragments before the ops loop starts")
	}
}
