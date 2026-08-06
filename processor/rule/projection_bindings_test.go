package rule

import (
	"context"
	"reflect"
	"testing"

	"github.com/c360studio/semstreams/pkg/projection"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/stretchr/testify/require"
)

// TestProcessorProjectionBindings verifies the processor returns the immutable
// effective pack-level authorization only after preflight.
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
				Mode:       projection.ModeReconcile,
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
	if err := rp.PreflightProjectionMutations(); err != nil {
		t.Fatalf("PreflightProjectionMutations: %v", err)
	}

	gotPack, gotContracts := rp.ProjectionBindings()
	if gotPack != "drone-ops-v1" {
		t.Errorf("packID: got %q want %q", gotPack, "drone-ops-v1")
	}
	if !reflect.DeepEqual(gotContracts, contracts) {
		t.Errorf("contracts mismatch:\n got %#v\nwant %#v", gotContracts, contracts)
	}
}

func TestProcessorSetPredicateReconcilerIsOneTimeAfterPreflight(t *testing.T) {
	t.Parallel()
	cfg := mustTestConfig(t, "one-time-replacer")
	cfg.ProjectionContracts = reconcileTestContracts(t)
	processor, err := NewProcessor(nil, &cfg)
	require.NoError(t, err)

	first := &capturingPredicateReconciler{}
	second := &capturingPredicateReconciler{}
	require.ErrorContains(t, processor.SetPredicateReconciler(first), "preflight has not completed")
	require.NoError(t, processor.PreflightProjectionMutations())
	require.NoError(t, processor.SetPredicateReconciler(first))
	require.ErrorContains(t, processor.SetPredicateReconciler(second), "already configured")
	require.Same(t, first, processor.reconciler)

	_, err = processor.reconciler.Reconcile(
		context.Background(),
		projection.ReconcileMutation{},
	)
	require.NoError(t, err)
	require.Len(t, first.requests, 1)
	require.Empty(t, second.requests)
}

// TestProcessorProjectionBindings_ContractsRequirePack verifies projection
// a rule-pack mutation binding cannot be constructed without its required pack identity.
func TestProcessorProjectionBindings_ContractsRequirePack(t *testing.T) {
	t.Parallel()

	cfg := defaultConfig()
	cfg.ProjectionContracts = []projection.Contract{{Name: "missing-pack"}}
	if _, err := NewProcessor(nil, &cfg); err == nil {
		t.Fatal("NewProcessor accepted projection contracts without pack_id")
	}
}
