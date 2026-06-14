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

	cfg := DefaultConfig()
	cfg.PackID = "drone-ops.v1"
	cfg.ProjectionContracts = contracts

	rp, err := NewProcessor(nil, &cfg)
	if err != nil {
		t.Fatalf("NewProcessor: %v", err)
	}

	gotPack, gotContracts := rp.ProjectionBindings()
	if gotPack != "drone-ops.v1" {
		t.Errorf("packID: got %q want %q", gotPack, "drone-ops.v1")
	}
	if !reflect.DeepEqual(gotContracts, contracts) {
		t.Errorf("contracts mismatch:\n got %#v\nwant %#v", gotContracts, contracts)
	}
}

// TestProcessorProjectionBindings_NoPack verifies the no-pack case returns an
// empty declaration so the composition root binds nothing.
func TestProcessorProjectionBindings_NoPack(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig() // no PackID, no contracts
	rp, err := NewProcessor(nil, &cfg)
	if err != nil {
		t.Fatalf("NewProcessor: %v", err)
	}

	gotPack, gotContracts := rp.ProjectionBindings()
	if gotPack != "" {
		t.Errorf("packID: got %q want empty", gotPack)
	}
	if len(gotContracts) != 0 {
		t.Errorf("contracts: got %d want 0", len(gotContracts))
	}
}
