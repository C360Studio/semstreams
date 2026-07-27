package rule

import (
	"context"
	"reflect"
	"testing"

	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/projection"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/stretchr/testify/require"
)

// TestProcessorProjectionBindings verifies the processor returns the pack-level
// ownership declaration it was constructed with, unchanged. The processor is
// substrate-agnostic: ProjectionBindings is a pure read of the stored config
// (ADR-056 #278 inc 2).
func TestProcessorProjectionBindings(t *testing.T) {
	t.Parallel()
	vocabulary.Register("drone.status.state")

	contracts := []projection.Contract{
		{
			Name:          "drone.status",
			MessageType:   "telemetry.robotics.drone-status.v1",
			EntityPattern: "acme.ops.robotics.gcs.drone.*",
			Groups: []projection.PredicateGroup{{
				Name:       "state",
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

func TestProcessorSetOwnedReplacerIsOneTimeAfterPreflight(t *testing.T) {
	t.Parallel()
	cfg := mustTestConfig(t, "one-time-replacer")
	cfg.ProjectionContracts = replaceOwnedTestContracts(t)
	processor, err := NewProcessor(nil, &cfg)
	require.NoError(t, err)

	first := &capturingOwnedReplacer{}
	second := &capturingOwnedReplacer{}
	require.ErrorContains(t, processor.SetOwnedReplacer(first), "preflight has not completed")
	require.NoError(t, processor.PreflightProjectionMutations())
	require.NoError(t, processor.SetOwnedReplacer(first))
	require.ErrorContains(t, processor.SetOwnedReplacer(second), "already configured")
	require.Same(t, first, processor.ownedReplacer)

	_, err = processor.ownedReplacer.ReplaceOwned(
		context.Background(),
		projection.ReplaceOwnedMutation{},
	)
	require.NoError(t, err)
	require.Len(t, first.requests, 1)
	require.Empty(t, second.requests)
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
