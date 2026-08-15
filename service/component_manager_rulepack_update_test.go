package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/types"
	"github.com/stretchr/testify/require"
)

type rulePackLifecycleProbe struct {
	baseDiscoverable
	starts int
	stops  int
}

func (p *rulePackLifecycleProbe) Initialize() error {
	return nil
}

func (p *rulePackLifecycleProbe) Start(context.Context) error {
	p.starts++
	return nil
}

func (p *rulePackLifecycleProbe) Stop(context.Context) error {
	p.stops++
	return nil
}

func TestComponentManagerRejectsRulePackConfigMutationBeforeLifecycleEffects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		accepted json.RawMessage
		proposed json.RawMessage
		full     config.ComponentConfigs
	}{
		{
			name:     "pack identity change",
			accepted: json.RawMessage(`{"pack_id":"stable-pack"}`),
			proposed: json.RawMessage(`{"pack_id":"changed-pack"}`),
		},
		{
			name:     "same pack structural component change",
			accepted: json.RawMessage(`{"pack_id":"stable-pack","enable_graph_integration":false}`),
			proposed: json.RawMessage(`{"pack_id":"stable-pack","enable_graph_integration":true}`),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			probe := &rulePackLifecycleProbe{baseDiscoverable: baseDiscoverable{name: "rule-processor"}}
			accepted := rulePackComponentConfig(true, test.accepted)
			proposed := rulePackComponentConfig(true, test.proposed)
			cm := newRulePackUpdateTestManager("rules", accepted, probe)
			full := &config.Config{Components: config.ComponentConfigs{"rules": proposed}}

			cm.handleComponentConfigUpdate(context.Background(), "rules", proposed, full)

			require.Zero(t, probe.starts)
			require.Zero(t, probe.stops)
			require.Same(t, probe, cm.components["rules"].Component)
			require.True(t, cm.components["rules"].Config.Equal(accepted))
			require.True(t, cm.rulePackConfigs["rules"].Equal(accepted))
		})
	}
}

func TestComponentManagerRejectsDuplicateRulePackBeforeStoppingEitherInstance(t *testing.T) {
	t.Parallel()

	probeA := &rulePackLifecycleProbe{baseDiscoverable: baseDiscoverable{name: "rule-processor"}}
	probeB := &rulePackLifecycleProbe{baseDiscoverable: baseDiscoverable{name: "rule-processor"}}
	acceptedA := rulePackComponentConfig(true, json.RawMessage(`{"pack_id":"pack-a"}`))
	acceptedB := rulePackComponentConfig(true, json.RawMessage(`{"pack_id":"pack-b"}`))
	proposedB := rulePackComponentConfig(true, json.RawMessage(`{"pack_id":"pack-a"}`))
	cm := newRulePackUpdateTestManager("rules-a", acceptedA, probeA)
	cm.components["rules-b"] = &component.ManagedComponent{
		Component: probeB,
		State:     component.StateStarted,
		Config:    acceptedB,
	}
	cm.rulePackConfigs["rules-b"] = acceptedB
	full := &config.Config{Components: config.ComponentConfigs{
		"rules-a": acceptedA,
		"rules-b": proposedB,
	}}

	cm.handleComponentConfigUpdate(context.Background(), "rules-b", proposedB, full)

	require.Zero(t, probeA.stops)
	require.Zero(t, probeB.stops)
	require.Same(t, probeA, cm.components["rules-a"].Component)
	require.Same(t, probeB, cm.components["rules-b"].Component)
	require.True(t, cm.rulePackConfigs["rules-b"].Equal(acceptedB))
}

func TestComponentManagerBulkReconcileRejectsRulePackChangeBeforeLifecycleEffects(t *testing.T) {
	t.Parallel()

	probeA := &rulePackLifecycleProbe{baseDiscoverable: baseDiscoverable{name: "rule-processor"}}
	probeB := &rulePackLifecycleProbe{baseDiscoverable: baseDiscoverable{name: "rule-processor"}}
	acceptedA := rulePackComponentConfig(true, json.RawMessage(`{"pack_id":"pack-a"}`))
	acceptedB := rulePackComponentConfig(true, json.RawMessage(`{"pack_id":"pack-b"}`))
	proposedB := rulePackComponentConfig(true, json.RawMessage(`{"pack_id":"pack-a"}`))
	cm := newRulePackUpdateTestManager("rules-a", acceptedA, probeA)
	cm.components["rules-b"] = &component.ManagedComponent{
		Component: probeB,
		State:     component.StateStarted,
		Config:    acceptedB,
	}
	cm.componentConfigs["rules-b"] = acceptedB
	cm.rulePackConfigs["rules-b"] = acceptedB
	desired := config.NewSafeConfig(&config.Config{Components: config.ComponentConfigs{
		"rules-a": acceptedA,
		"rules-b": proposedB,
	}})

	cm.reconcileComponents(context.Background(), desired)

	require.Zero(t, probeA.stops)
	require.Zero(t, probeB.stops)
	require.Same(t, probeA, cm.components["rules-a"].Component)
	require.Same(t, probeB, cm.components["rules-b"].Component)
	require.True(t, cm.rulePackConfigs["rules-b"].Equal(acceptedB))
}

func TestUnrelatedComponentUpdateRejectsForbiddenRulePackSnapshotBeforeLifecycleEffects(t *testing.T) {
	t.Parallel()

	probe := &rulePackLifecycleProbe{baseDiscoverable: baseDiscoverable{name: "other-processor"}}
	accepted := types.ComponentConfig{
		Type: types.ComponentTypeProcessor, Name: "other-processor", Enabled: true, Config: json.RawMessage(`{"value":1}`),
	}
	proposed := types.ComponentConfig{
		Type: types.ComponentTypeProcessor, Name: "other-processor", Enabled: true, Config: json.RawMessage(`{"value":2}`),
	}
	cm := newRulePackUpdateTestManager("other", accepted, probe)
	newRulePack := rulePackComponentConfig(true, json.RawMessage(`{"pack_id":"new-pack"}`))
	full := &config.Config{Components: config.ComponentConfigs{
		"other":     proposed,
		"new-rules": newRulePack,
	}}

	cm.handleComponentConfigUpdate(context.Background(), "other", proposed, full)

	require.Zero(t, probe.starts)
	require.Zero(t, probe.stops)
	require.Same(t, probe, cm.components["other"].Component)
	require.True(t, cm.components["other"].Config.Equal(accepted))
}

func TestRestartComponentRejectsRuleProcessorAsDefenseInDepth(t *testing.T) {
	t.Parallel()

	probe := &rulePackLifecycleProbe{baseDiscoverable: baseDiscoverable{name: "rule-processor"}}
	accepted := rulePackComponentConfig(true, json.RawMessage(`{"pack_id":"stable-pack"}`))
	cm := newRulePackUpdateTestManager("rules", accepted, probe)

	err := cm.restartComponentWithNewConfig(
		context.Background(),
		"rules",
		accepted,
		cm.components["rules"],
	)

	require.ErrorContains(t, err, "pack ownership is bound before ComponentManager.Start")
	require.Zero(t, probe.stops)
	require.Same(t, probe, cm.components["rules"].Component)
}

func newRulePackUpdateTestManager(
	name string,
	accepted types.ComponentConfig,
	probe component.Discoverable,
) *ComponentManager {
	configs := config.ComponentConfigs{name: accepted}
	return &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		registry:    component.NewRegistry(),
		components: map[string]*component.ManagedComponent{
			name: {
				Component: probe,
				State:     component.StateStarted,
				Config:    accepted,
			},
		},
		componentConfigs: configs,
		rulePackConfigs:  cloneRulePackConfigs(configs),
	}
}

func rulePackComponentConfig(enabled bool, raw json.RawMessage) types.ComponentConfig {
	return types.ComponentConfig{
		Type:    types.ComponentTypeProcessor,
		Name:    "rule-processor",
		Enabled: enabled,
		Config:  raw,
	}
}
