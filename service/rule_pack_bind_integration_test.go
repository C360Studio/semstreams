//go:build integration

package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/projection"
	rule "github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/service"
	"github.com/c360studio/semstreams/types"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/stretchr/testify/require"
)

const (
	droneStatusStatePredicate       = "drone.status.state"
	missionStatusPhasePredicate     = "mission.status.phase"
	sensorStatusCalibratedPredicate = "sensor.status.calibrated"
)

func init() {
	vocabulary.Register(droneStatusStatePredicate)
	vocabulary.Register(missionStatusPhasePredicate)
	vocabulary.Register(sensorStatusCalibratedPredicate)
}

// ruleComponentConfig builds the operator-facing component config for a rule
// processor carrying a pack_id and projection contracts. The Config field is a
// rule.Config JSON; the rule factory copies pack_id/projection_contracts off it
// (the production wire under test).
func ruleComponentConfig(t *testing.T, packID string, contracts []projection.Contract) types.ComponentConfig {
	t.Helper()
	rc, err := rule.NewConfig(packID)
	require.NoError(t, err)
	rc.EnableGraphIntegration = false // keep the (unstarted) processor lean
	rc.ProjectionContracts = contracts
	raw, err := json.Marshal(rc)
	require.NoError(t, err)
	return types.ComponentConfig{
		Type:    types.ComponentTypeProcessor,
		Name:    "rule-processor",
		Enabled: true,
		Config:  raw,
	}
}

// newManagerWithRuleComponents drives the PRODUCTION wire: a real config
// Manager holding the given rule component configs, a component registry with
// the real rule processor registered, and a service.Manager whose mandatory
// "component-manager" service constructs (but does NOT Start) those rule
// processors. Returns the wired service.Manager. NOTHING is started — so
// ProjectionBindings is read on freshly-constructed, never-Started processors,
// matching the composition-root bind-before-StartAll invariant.
func newManagerWithRuleComponents(t *testing.T, ctx context.Context, tc *natsclient.TestClient, comps config.ComponentConfigs) *service.Manager {
	t.Helper()

	cfgManager, err := config.NewConfigManager(&config.Config{
		Platform: config.PlatformConfig{
			Org: "test", ID: "test-platform", InstanceID: "test-001", Environment: "test",
		},
		Components: comps,
	}, tc.Client, slog.Default())
	require.NoError(t, err)
	require.NoError(t, cfgManager.PushToKV(ctx))
	require.NoError(t, cfgManager.Start(ctx))
	t.Cleanup(func() { _ = cfgManager.Stop(5 * time.Second) })

	compRegistry := componentRegistryWithRule(t)

	deps := &service.Dependencies{
		NATSClient:        tc.Client,
		Manager:           cfgManager,
		Logger:            slog.Default(),
		ComponentRegistry: compRegistry,
	}

	svcRegistry := service.NewServiceRegistry()
	svcRegistry.Register("component-manager", service.NewComponentManager)
	manager := service.NewServiceManager(svcRegistry)
	require.NoError(t, manager.ConfigureFromServices(
		map[string]types.ServiceConfig{}, deps))

	// CreateService("component-manager") runs the ComponentManager constructor,
	// which constructs every enabled component (the rule processors) WITHOUT
	// starting them. After this, manager.ProjectionBinders() returns them.
	_, err = manager.CreateService("component-manager",
		json.RawMessage(`{"watch_config": false}`), deps)
	require.NoError(t, err)

	return manager
}

// componentRegistryWithRule returns a component registry with the real rule
// processor factory registered.
func componentRegistryWithRule(t *testing.T) *component.Registry {
	t.Helper()
	r := component.NewRegistry()
	require.NoError(t, rule.Register(r))
	return r
}

func newOwnershipRegistryForBind(t *testing.T, ctx context.Context, tc *natsclient.TestClient) *ownership.Registry {
	t.Helper()
	reg, err := ownership.EnsureBuckets(ctx, tc.Client, slog.Default(), vocabulary.InverseResolver)
	require.NoError(t, err)
	return reg
}

func droneStatusContract() projection.Contract {
	return projection.Contract{
		Name:          "drone.status",
		MessageType:   "telemetry.robotics.drone-status.v1",
		EntityPattern: "acme.ops.robotics.gcs.drone.*",
		Groups: []projection.PredicateGroup{{
			Mode:       ownership.ModeReplaceOwned,
			Predicates: []string{droneStatusStatePredicate},
		}},
	}
}

// TestBindRulePackContracts_ClaimInEpochBeforeStart drives the production entry
// service.BindRulePackContracts (NOT a hand-rolled bind, per
// [[feedback_integration_tests_must_drive_production_wire]]) and proves the
// rule pack's claim lands in the ownership epoch BEFORE any processor Start().
func TestBindRulePackContracts_ClaimInEpochBeforeStart(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer tc.Terminate()

	ownerReg := newOwnershipRegistryForBind(t, ctx, tc)
	hb := ownerReg.NewHeartbeater(ownership.HeartbeatInterval)

	manager := newManagerWithRuleComponents(t, ctx, tc, config.ComponentConfigs{
		"rule-processor": ruleComponentConfig(t, "drone-ops-v1",
			[]projection.Contract{droneStatusContract()}),
	})

	// Production entry. No Start() / StartAll() anywhere in this test.
	require.NoError(t, service.BindRulePackContracts(ctx, manager, ownerReg, hb, slog.Default()))

	owner, ok, err := ownerReg.OwnerOf(ctx,
		"acme.ops.robotics.gcs.drone.001", droneStatusStatePredicate)
	require.NoError(t, err)
	require.True(t, ok, "claim must be in the epoch after bind")
	require.Equal(t, "rule-pack.drone-ops-v1", owner)
	require.True(t, hb.IsEnrolled("rule-pack.drone-ops-v1"),
		"a bound static owner must be heartbeat-enrolled")
}

// TestBindRulePackContracts_OverlapLoggedNotAborted proves a second pack whose
// contract overlaps an already-claimed cell is rejected by the substrate with
// ErrOwnershipOverlap, the helper logs+continues (observe-only), and the FIRST
// owner still holds the cell.
func TestBindRulePackContracts_OverlapLoggedNotAborted(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer tc.Terminate()

	ownerReg := newOwnershipRegistryForBind(t, ctx, tc)
	hb := ownerReg.NewHeartbeater(ownership.HeartbeatInterval)

	// Pre-bind pack-a directly through the production helper.
	managerA := newManagerWithRuleComponents(t, ctx, tc, config.ComponentConfigs{
		"rule-processor": ruleComponentConfig(t, "pack-a",
			[]projection.Contract{droneStatusContract()}),
	})
	require.NoError(t, service.BindRulePackContracts(ctx, managerA, ownerReg, hb, slog.Default()))

	// pack-b declares the SAME owned cell — overlap.
	overlapping := droneStatusContract()
	overlapping.Name = "drone.status.poach"
	managerB := newManagerWithRuleComponents(t, ctx, tc, config.ComponentConfigs{
		"rule-processor": ruleComponentConfig(t, "pack-b",
			[]projection.Contract{overlapping}),
	})

	// Must NOT panic / abort — helper swallows the overlap as observe-only.
	require.NoError(t, service.BindRulePackContracts(ctx, managerB, ownerReg, hb, slog.Default()))

	// The cell still belongs to pack-a; pack-b was rejected.
	owner, ok, err := ownerReg.OwnerOf(ctx,
		"acme.ops.robotics.gcs.drone.001", droneStatusStatePredicate)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "rule-pack.pack-a", owner)
	require.False(t, hb.IsEnrolled("rule-pack.pack-b"),
		"a rejected (overlapping) owner must not be heartbeat-enrolled")

	// Sanity: the substrate genuinely treats this as an overlap (the helper hides
	// it, so assert against a direct derive to keep the test honest).
	_, derr := projection.Derive("rule-pack.pack-b", overlapping)
	require.NoError(t, derr, "derive itself is valid; the overlap is cross-owner at register time")
	_, berr := projection.Bind(ctx, ownerReg, "rule-pack.pack-c", overlapping)
	require.True(t, errors.Is(berr, ownership.ErrOwnershipOverlap),
		"a fresh owner binding the same cell must hit ErrOwnershipOverlap")
}

// TestBindRulePackContracts_MultiPackAndDuplicate proves two rule components
// with DISTINCT pack_ids bind two distinct owners, while a duplicate pack_id
// in one composition hard-fails before either duplicate is activated.
func TestBindRulePackContracts_MultiPackAndDuplicate(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer tc.Terminate()

	ownerReg := newOwnershipRegistryForBind(t, ctx, tc)
	hb := ownerReg.NewHeartbeater(ownership.HeartbeatInterval)

	missionContract := projection.Contract{
		Name:          "mission.status",
		EntityPattern: "acme.ops.robotics.gcs.mission.*",
		Groups: []projection.PredicateGroup{{
			Mode:       ownership.ModeReplaceOwned,
			Predicates: []string{missionStatusPhasePredicate},
		}},
	}

	// Two distinct pack_ids in one manager.
	manager := newManagerWithRuleComponents(t, ctx, tc, config.ComponentConfigs{
		"rule-drones":   ruleComponentConfig(t, "drone-ops-v1", []projection.Contract{droneStatusContract()}),
		"rule-missions": ruleComponentConfig(t, "mission-ops-v1", []projection.Contract{missionContract}),
	})

	require.NoError(t, service.BindRulePackContracts(ctx, manager, ownerReg, hb, slog.Default()))

	require.True(t, hb.IsEnrolled("rule-pack.drone-ops-v1"))
	require.True(t, hb.IsEnrolled("rule-pack.mission-ops-v1"))

	dOwner, dOK, err := ownerReg.OwnerOf(ctx, "acme.ops.robotics.gcs.drone.001", droneStatusStatePredicate)
	require.NoError(t, err)
	require.True(t, dOK)
	require.Equal(t, "rule-pack.drone-ops-v1", dOwner)

	mOwner, mOK, err := ownerReg.OwnerOf(ctx, "acme.ops.robotics.gcs.mission.alpha", missionStatusPhasePredicate)
	require.NoError(t, err)
	require.True(t, mOK)
	require.Equal(t, "rule-pack.mission-ops-v1", mOwner)

	// Duplicate pack_id across two enabled components is a boot error. The
	// complete binder set is preflighted before any duplicate owner is minted or
	// enrolled.
	dupManager := newManagerWithRuleComponents(t, ctx, tc, config.ComponentConfigs{
		"rule-a": ruleComponentConfig(t, "dup-pack", []projection.Contract{missionContract2()}),
		"rule-b": ruleComponentConfig(t, "dup-pack", []projection.Contract{missionContract2()}),
	})
	err = service.BindRulePackContracts(ctx, dupManager, ownerReg, hb, slog.Default())
	require.ErrorContains(t, err, `duplicate enabled rule pack_id "dup-pack"`)
	require.False(t, hb.IsEnrolled("rule-pack.dup-pack"))
}

func TestRulePackMissingPackIDRejectedAtFactory(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer tc.Terminate()

	raw := json.RawMessage(`{"enable_graph_integration":false}`)
	manager := newManagerWithRuleComponents(t, ctx, tc, config.ComponentConfigs{
		"rule-missing-pack": {
			Type: types.ComponentTypeProcessor, Name: "rule-processor", Enabled: true, Config: raw,
		},
	})
	require.Empty(t, manager.ProjectionBinders(), "missing pack_id must prevent processor construction")
}

// TestRulePackInvalidPackID_RejectedAtFactory proves an illegal pack_id is
// rejected by the production rule factory (config validation) — it never
// reaches the bind helper / RegisterOwner. Drives CreateRuleProcessor, the
// production component-construction entry, through CreateService.
func TestRulePackInvalidPackID_RejectedAtFactory(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer tc.Terminate()
	invalidRuleConfig, err := rule.NewConfig("valid-test-pack")
	require.NoError(t, err)
	invalidRuleConfig.PackID = "bad:pack"
	invalidRuleConfig.ProjectionContracts = []projection.Contract{droneStatusContract()}
	invalidRaw, err := json.Marshal(invalidRuleConfig)
	require.NoError(t, err)

	cfgManager, err := config.NewConfigManager(&config.Config{
		Platform: config.PlatformConfig{
			Org: "test", ID: "test-platform", InstanceID: "test-001", Environment: "test",
		},
		Components: config.ComponentConfigs{
			"rule-processor": {
				Type: types.ComponentTypeProcessor, Name: "rule-processor", Enabled: true, Config: invalidRaw,
			},
		},
	}, tc.Client, slog.Default())
	require.NoError(t, err)
	require.NoError(t, cfgManager.PushToKV(ctx))
	require.NoError(t, cfgManager.Start(ctx))
	t.Cleanup(func() { _ = cfgManager.Stop(5 * time.Second) })

	deps := &service.Dependencies{
		NATSClient:        tc.Client,
		Manager:           cfgManager,
		Logger:            slog.Default(),
		ComponentRegistry: componentRegistryWithRule(t),
	}
	svcRegistry := service.NewServiceRegistry()
	svcRegistry.Register("component-manager", service.NewComponentManager)
	manager := service.NewServiceManager(svcRegistry)
	require.NoError(t, manager.ConfigureFromServices(map[string]types.ServiceConfig{}, deps))

	// The ComponentManager constructor builds components; the rule factory must
	// reject the illegal pack_id. Whether the rejection surfaces as a
	// CreateService error or a never-constructed component, the invariant is the
	// same: no rule.Processor with pack_id "bad:pack" exists to bind.
	_, _ = manager.CreateService("component-manager",
		json.RawMessage(`{"watch_config": false}`), deps)

	for _, b := range manager.ProjectionBinders() {
		packID, _ := b.ProjectionBindings()
		require.NotEqual(t, "bad:pack", packID,
			"an illegal pack_id must never reach a constructed processor / the bind helper")
	}
}

func missionContract2() projection.Contract {
	return projection.Contract{
		Name:          "dup.status",
		EntityPattern: "acme.ops.robotics.gcs.sensor.*",
		Groups: []projection.PredicateGroup{{
			Mode:       ownership.ModeReplaceOwned,
			Predicates: []string{sensorStatusCalibratedPredicate},
		}},
	}
}
