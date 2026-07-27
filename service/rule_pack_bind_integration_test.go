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
	"github.com/c360studio/semstreams/internal/builtinprojection"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/projection"
	rule "github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/service"
	"github.com/c360studio/semstreams/types"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/c360studio/semstreams/vocabulary/builtins"
	"github.com/stretchr/testify/require"
)

const (
	droneStatusStatePredicate       = "drone.status.state"
	missionStatusPhasePredicate     = "mission.status.phase"
	sensorStatusCalibratedPredicate = "sensor.status.calibrated"
)

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

func derivedRuleComponentConfig(
	t *testing.T,
	packID string,
	definitions []rule.Definition,
) types.ComponentConfig {
	t.Helper()
	rc, err := rule.NewConfig(packID)
	require.NoError(t, err)
	rc.EnableGraphIntegration = false
	rc.InlineRules = definitions
	raw, err := json.Marshal(rc)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "projection_contracts")
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
// processors. Returns the wired service.Manager. NOTHING is started; the
// composition root preflights each frozen rule snapshot before it reads the
// effective ProjectionBindings and binds before StartAll.
func newManagerWithRuleComponents(t *testing.T, ctx context.Context, tc *natsclient.TestClient, comps config.ComponentConfigs) *service.Manager {
	t.Helper()
	manager, err := createManagerWithRuleComponents(t, ctx, tc, comps)
	require.NoError(t, err)
	return manager
}

func createManagerWithRuleComponents(
	t *testing.T,
	ctx context.Context,
	tc *natsclient.TestClient,
	comps config.ComponentConfigs,
) (*service.Manager, error) {
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
	return manager, err
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
	vocabulary.Register(droneStatusStatePredicate)
	return projection.Contract{
		Name:          "drone.status",
		MessageType:   "telemetry.robotics.drone-status.v1",
		EntityPattern: "acme.ops.robotics.gcs.drone.*",
		Groups: []projection.PredicateGroup{{
			Name:       "state",
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

func TestBindRulePackContracts_DerivedClaimInEpochBeforeStart(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer tc.Terminate()
	vocabulary.Register(droneStatusStatePredicate)

	ownerReg := newOwnershipRegistryForBind(t, ctx, tc)
	hb := ownerReg.NewHeartbeater(ownership.HeartbeatInterval)
	manager := newManagerWithRuleComponents(t, ctx, tc, config.ComponentConfigs{
		"rule-derived": derivedRuleComponentConfig(t, "derived-drone-v1", []rule.Definition{{
			ID:      "derive-drone-status",
			Type:    "expression",
			Name:    "derive drone status",
			Enabled: false,
			Entity:  rule.EntityConfig{Pattern: "acme.ops.robotics.gcs.drone.*"},
			OnEnter: []rule.Action{{
				Type:               rule.ActionTypeReplaceOwned,
				ProjectionContract: "drone.status",
				ProjectionGroup:    "state",
				Predicate:          droneStatusStatePredicate,
				Object:             "ready",
			}},
		}}),
	})

	require.NoError(t, service.BindRulePackContracts(ctx, manager, ownerReg, hb, slog.Default()))
	owner, ok, err := ownerReg.OwnerOf(
		ctx,
		"acme.ops.robotics.gcs.drone.001",
		droneStatusStatePredicate,
	)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "rule-pack.derived-drone-v1", owner)
	require.True(t, hb.IsEnrolled("rule-pack.derived-drone-v1"))
}

func TestComponentManager_InvalidDerivedPackAbortsValidSiblingBeforeBinding(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer tc.Terminate()
	vocabulary.Register(droneStatusStatePredicate)

	ownerReg := newOwnershipRegistryForBind(t, ctx, tc)
	hb := ownerReg.NewHeartbeater(ownership.HeartbeatInterval)
	manager, err := createManagerWithRuleComponents(t, ctx, tc, config.ComponentConfigs{
		"valid-derived": derivedRuleComponentConfig(t, "valid-derived-v1", []rule.Definition{{
			ID:      "valid-derived",
			Type:    "expression",
			Name:    "valid derived",
			Enabled: false,
			Entity:  rule.EntityConfig{Pattern: "acme.ops.robotics.gcs.drone.*"},
			OnEnter: []rule.Action{{
				Type:               rule.ActionTypeReplaceOwned,
				ProjectionContract: "drone.status",
				ProjectionGroup:    "state",
				Predicate:          droneStatusStatePredicate,
			}},
		}}),
		"invalid-derived": derivedRuleComponentConfig(t, "invalid-derived-v1", []rule.Definition{{
			ID:      "invalid-derived",
			Type:    "expression",
			Name:    "invalid derived",
			Enabled: false,
			OnEnter: []rule.Action{{
				Type:               rule.ActionTypeReplaceOwned,
				ProjectionContract: "dynamic.status",
				ProjectionGroup:    "state",
				Predicate:          droneStatusStatePredicate,
				Subject:            "$entity.triple.parent_id", // predicate-audit:invalid {"kind":"stored-predicate","value":"parent_id","reason":"arity"}
			}},
		}}),
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid-derived")
	require.ErrorContains(t, err, "requires an explicit projection_contracts envelope")
	require.Empty(t, manager.ProjectionBinders(),
		"failed component-manager construction must not expose a partial binder set")

	owner, found, ownerErr := ownerReg.OwnerOf(
		ctx,
		"acme.ops.robotics.gcs.drone.001",
		droneStatusStatePredicate,
	)
	require.NoError(t, ownerErr)
	require.False(t, found)
	require.Empty(t, owner)
	require.False(t, hb.IsEnrolled("rule-pack.valid-derived-v1"))
	require.False(t, hb.IsEnrolled("rule-pack.invalid-derived-v1"))
}

func TestComponentManager_DisabledInvalidRulePackIsIgnored(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer tc.Terminate()
	vocabulary.Register(droneStatusStatePredicate)

	disabledInvalid := types.ComponentConfig{
		Type: types.ComponentTypeProcessor, Name: "rule-processor", Enabled: false,
		Config: json.RawMessage(`{"pack_id":"bad:pack"}`),
	}
	manager, err := createManagerWithRuleComponents(t, ctx, tc, config.ComponentConfigs{
		"valid-derived": derivedRuleComponentConfig(t, "valid-disabled-sibling-v1", []rule.Definition{{
			ID:      "valid-disabled-sibling",
			Type:    "expression",
			Name:    "valid disabled sibling",
			Enabled: false,
			Entity:  rule.EntityConfig{Pattern: "acme.ops.robotics.gcs.drone.*"},
			OnEnter: []rule.Action{{
				Type:               rule.ActionTypeReplaceOwned,
				ProjectionContract: "drone.status",
				ProjectionGroup:    "state",
				Predicate:          droneStatusStatePredicate,
			}},
		}}),
		"disabled-invalid": disabledInvalid,
	})
	require.NoError(t, err)
	require.Len(t, manager.ProjectionBinders(), 1)

	ownerReg := newOwnershipRegistryForBind(t, ctx, tc)
	hb := ownerReg.NewHeartbeater(ownership.HeartbeatInterval)
	require.NoError(t, service.BindRulePackContracts(ctx, manager, ownerReg, hb, slog.Default()))
	require.True(t, hb.IsEnrolled("rule-pack.valid-disabled-sibling-v1"))
	require.False(t, hb.IsEnrolled("rule-pack.bad:pack"))
}

func TestBindRulePackContracts_OwnerAlreadyBoundFailsClosed(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer tc.Terminate()

	ownerReg := newOwnershipRegistryForBind(t, ctx, tc)
	hb := ownerReg.NewHeartbeater(ownership.HeartbeatInterval)
	manager := newManagerWithRuleComponents(t, ctx, tc, config.ComponentConfigs{
		"rule-processor": ruleComponentConfig(
			t,
			"single-bind-v1",
			[]projection.Contract{droneStatusContract()},
		),
	})

	require.NoError(
		t,
		service.BindRulePackContracts(ctx, manager, ownerReg, hb, slog.Default()),
	)
	claims, claimsErr := tc.Client.GetKeyValueBucket(ctx, ownership.BucketOwnerClaims)
	require.NoError(t, claimsErr)
	presence, presenceErr := tc.Client.GetKeyValueBucket(ctx, ownership.BucketOwnerPresence)
	require.NoError(t, presenceErr)
	beforeClaims, claimsErr := claims.Get(ctx, "_registry")
	require.NoError(t, claimsErr)
	beforePresence, presenceErr := presence.Get(ctx, "heartbeat.rule-pack.single-bind-v1")
	require.NoError(t, presenceErr)

	err := service.BindRulePackContracts(ctx, manager, ownerReg, hb, slog.Default())
	require.ErrorIs(t, err, ownership.ErrOwnerAlreadyBound)
	owner, found, ownerErr := ownerReg.OwnerOf(
		ctx,
		"acme.ops.robotics.gcs.drone.001",
		droneStatusStatePredicate,
	)
	require.NoError(t, ownerErr)
	require.True(t, found)
	require.Equal(t, "rule-pack.single-bind-v1", owner)
	afterClaims, claimsErr := claims.Get(ctx, "_registry")
	require.NoError(t, claimsErr)
	afterPresence, presenceErr := presence.Get(ctx, "heartbeat.rule-pack.single-bind-v1")
	require.NoError(t, presenceErr)
	require.Equal(t, beforeClaims.Revision(), afterClaims.Revision(),
		"rejected repeat bind must not mutate ownership claims")
	require.Equal(t, beforePresence.Revision(), afterPresence.Revision(),
		"rejected repeat bind must not heartbeat again")
	require.True(t, hb.IsEnrolled("rule-pack.single-bind-v1"))
}

func TestBindRulePackContracts_StaleOverlapFailsClosed(t *testing.T) {
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

	err := service.BindRulePackContracts(ctx, managerB, ownerReg, hb, slog.Default())
	require.ErrorIs(t, err, ownership.ErrOwnershipOverlap)

	// The cell still belongs to pack-a; pack-b was rejected.
	owner, ok, err := ownerReg.OwnerOf(ctx,
		"acme.ops.robotics.gcs.drone.001", droneStatusStatePredicate)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "rule-pack.pack-a", owner)
	require.False(t, hb.IsEnrolled("rule-pack.pack-b"),
		"a rejected (overlapping) owner must not be heartbeat-enrolled")

	// Sanity: the substrate and the production helper expose the same overlap.
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
	vocabulary.Register(missionStatusPhasePredicate)

	ownerReg := newOwnershipRegistryForBind(t, ctx, tc)
	hb := ownerReg.NewHeartbeater(ownership.HeartbeatInterval)

	missionContract := projection.Contract{
		Name:          "mission.status",
		EntityPattern: "acme.ops.robotics.gcs.mission.*",
		Groups: []projection.PredicateGroup{{
			Name:       "phase",
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

func TestBindRulePackContracts_BuiltInOverlapFailsClosed(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer tc.Terminate()
	builtins.Register()

	ownerReg := newOwnershipRegistryForBind(t, ctx, tc)
	hb := ownerReg.NewHeartbeater(ownership.HeartbeatInterval)
	_, err := projection.BindMutationClient(ctx, projection.MutationClientConfig{
		NATS:        tc.Client,
		Registry:    ownerReg,
		Heartbeater: hb,
		Owner:       builtinprojection.OwnerID,
		Contracts:   builtinprojection.Contracts(),
	})
	require.NoError(t, err)

	manager := newManagerWithRuleComponents(t, ctx, tc, config.ComponentConfigs{
		"rule-built-in-overlap": ruleComponentConfig(
			t,
			"lesson-rule-pack",
			[]projection.Contract{builtinprojection.Contracts()[1]},
		),
	})
	err = service.BindRulePackContracts(ctx, manager, ownerReg, hb, slog.Default())
	require.ErrorIs(t, err, ownership.ErrOwnershipOverlap)
	require.False(t, hb.IsEnrolled("rule-pack.lesson-rule-pack"))
}

func TestRulePackMissingPackIDRejectedAtFactory(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer tc.Terminate()

	raw := json.RawMessage(`{"enable_graph_integration":false}`)
	manager, err := createManagerWithRuleComponents(t, ctx, tc, config.ComponentConfigs{
		"rule-missing-pack": {
			Type: types.ComponentTypeProcessor, Name: "rule-processor", Enabled: true, Config: raw,
		},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "rule-missing-pack")
	require.ErrorContains(t, err, "pack_id is required")
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

	manager, err := createManagerWithRuleComponents(t, ctx, tc, config.ComponentConfigs{
		"rule-processor": {
			Type: types.ComponentTypeProcessor, Name: "rule-processor", Enabled: true, Config: invalidRaw,
		},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "rule-processor")
	require.ErrorContains(t, err, "invalid pack_id")
	require.Empty(t, manager.ProjectionBinders(),
		"invalid pack_id must prevent partial component-manager construction")
}

func missionContract2() projection.Contract {
	vocabulary.Register(sensorStatusCalibratedPredicate)
	return projection.Contract{
		Name:          "dup.status",
		EntityPattern: "acme.ops.robotics.gcs.sensor.*",
		Groups: []projection.PredicateGroup{{
			Name:       "calibration",
			Mode:       ownership.ModeReplaceOwned,
			Predicates: []string{sensorStatusCalibratedPredicate},
		}},
	}
}
