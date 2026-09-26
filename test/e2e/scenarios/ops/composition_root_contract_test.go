package ops

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestE2ECompositionRootLoadsCheckedInPersonaFragments pins the one thing the
// ops tier needs from the framework boot beyond its lesson-curation control
// responder: the persona fragments must be loaded before the ops loop starts.
// Both framework binaries boot through internal/boot, so its Run is the one
// place the load can live. That the ops tier boots the E2E binary at all —
// target e2e, binary cmd/e2e-semstreams, its own image tag, its option
// variable — is pinned for every tier at once by
// test/contract/e2e_tier_binary_contract_test.go.
func TestE2ECompositionRootLoadsCheckedInPersonaFragments(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "../../../.."))

	bootRun, err := os.ReadFile(filepath.Join(root, "internal/boot/run.go"))
	if err != nil {
		t.Fatalf("read framework boot: %v", err)
	}
	if !strings.Contains(string(bootRun), `persona.LoadFromDirectory(bootCtx, "configs/personas/fragments", personaMgr, logger)`) {
		t.Error("the framework boot must load checked-in persona fragments before the ops loop starts")
	}
}
