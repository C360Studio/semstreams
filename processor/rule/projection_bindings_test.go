package rule

import (
	"reflect"
	"testing"

	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/projection"
)

// TestProcessorProjectionBindings verifies the processor returns the pack-level
// ownership declaration it was constructed with, unchanged. The processor is
// substrate-agnostic: ProjectionBindings is a pure read of the stored config
// (ADR-056 #278 inc 2).
func TestProcessorProjectionBindings(t *testing.T) {
	t.Parallel()

	contracts := []projection.Contract{
		{
			Name:          "drone.status",
			MessageType:   "telemetry.robotics.drone-status.v1",
			EntityPattern: "acme.ops.robotics.gcs.drone.*",
			Groups: []projection.PredicateGroup{{
				Mode:       ownership.ModeReplaceOwned,
				Predicates: []string{"drone.status.state"},
			}},
		},
	}

	cfg := mustTestConfig(t, "drone-ops-v1")
	cfg.ProjectionContracts = contracts

	rp, err := NewProcessor(nil, &cfg)
	if err != nil {
		t.Fatalf("NewProcessor: %v", err)
	}

	gotPack, gotContracts := rp.ProjectionBindings()
	if gotPack != "drone-ops-v1" {
		t.Errorf("packID: got %q want %q", gotPack, "drone-ops-v1")
	}
	if !reflect.DeepEqual(gotContracts, contracts) {
		t.Errorf("contracts mismatch:\n got %#v\nwant %#v", gotContracts, contracts)
	}
}

// TestProcessorProjectionBindings_ContractsRequirePack verifies projection
// ownership cannot be constructed without its required pack identity.
func TestProcessorProjectionBindings_ContractsRequirePack(t *testing.T) {
	t.Parallel()

	cfg := defaultConfig()
	cfg.ProjectionContracts = []projection.Contract{{Name: "missing-pack"}}
	if _, err := NewProcessor(nil, &cfg); err == nil {
		t.Fatal("NewProcessor accepted projection contracts without pack_id")
	}
}
