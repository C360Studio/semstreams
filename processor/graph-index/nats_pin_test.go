package graphindex

import (
	"encoding/json"
	"os/exec"
	"testing"
)

// One pin for this package's evidence gates (owner-filter load, predicate-
// layout smoke). Both integration harnesses alias these constants, so the
// package cannot carry two divergent pins — the 2026-08-02 audit found both
// gates running server 2.12.4 + reporting SDK v1.48.0 while go.mod built
// v1.52.0 and the harness docs claimed 2.14.4: the evidence log lines were
// silently false. The server fragment is also matched by the repo-wide
// convergence guard (test/contract/nats_version_contract_test.go), which is
// what makes the NEXT bump loud here instead of silent.
//
// Digest is the same normative pin natsclient's KV key contract runs
// (natsclient/kv_key_contract_integration_test.go).
const (
	graphIndexNATSServerPin = "2.14.4-alpine@sha256:f2123f533c2b0cada0a5c5ec434fb2b8cfe1cf220215ef9d7517e1372917ad66"
	graphIndexNATSGoPin     = "v1.52.0"
)

// TestGraphIndexPinnedNATSGoMatchesResolvedModule keeps the REPORTED SDK
// version honest against the module the build actually resolves — the same
// discipline natsclient applies to its own evidence constant
// (TestKVContractPinnedNATSGoDependency). Runs in the unit tier so drift is
// caught on every CI run, not only when the heavy gates are exercised.
func TestGraphIndexPinnedNATSGoMatchesResolvedModule(t *testing.T) {
	t.Parallel()

	out, err := exec.Command("go", "list", "-m", "-json", "github.com/nats-io/nats.go").Output()
	if err != nil {
		t.Fatalf("resolve nats.go module: %v", err)
	}
	var mod struct {
		Version string `json:"Version"`
	}
	if err := json.Unmarshal(out, &mod); err != nil {
		t.Fatalf("decode resolved nats.go module: %v", err)
	}
	if mod.Version != graphIndexNATSGoPin {
		t.Fatalf("graphIndexNATSGoPin = %q but the build resolves nats.go %q — the evidence "+
			"log lines of this package's gates would report a version the binary does not use; "+
			"update the pin with the dependency bump", graphIndexNATSGoPin, mod.Version)
	}
}
