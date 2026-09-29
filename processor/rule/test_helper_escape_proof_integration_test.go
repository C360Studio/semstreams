//go:build integration

package rule

import (
	"context"
	"testing"
	"time"
)

func TestRevisionClaimHarnessSetupEscapeRunsConcreteStop(t *testing.T) {
	operationCtx, cancelOperation := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancelOperation()
	var escapedOwner *graphIngestTestOwner
	const setupEscape = "revision harness setup escape"
	func() {
		defer func() {
			if got := recover(); got != setupEscape {
				t.Errorf("setup escape panic = %v, want %q", got, setupEscape)
			}
		}()
		newRevisionClaimHarness(operationCtx, t, func(owner *graphIngestTestOwner) {
			escapedOwner = owner
			panic(setupEscape)
		})
	}()
	if escapedOwner == nil || !escapedOwner.attempted || escapedOwner.transferred {
		t.Fatalf("actual revision helper did not finish provisional concrete graph-ingest owner: %+v", escapedOwner)
	}
	if err := operationCtx.Err(); err != nil {
		t.Fatalf("operation authority ended during concrete provisional Stop: %v", err)
	}
}
