package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
	"github.com/stretchr/testify/require"
)

type rulePackInitProbe struct {
	baseDiscoverable
	initializeErr error
	startCalls    int
}

func (probe *rulePackInitProbe) Initialize() error {
	return probe.initializeErr
}

func (probe *rulePackInitProbe) Start(context.Context) error {
	probe.startCalls++
	return nil
}

func (*rulePackInitProbe) Stop(time.Duration) error {
	return nil
}

type rulePackInitFixture struct {
	FactoryError    string `json:"factory_error"`
	InitializeError string `json:"initialize_error"`
}

func newRulePackColdBootManager(
	t *testing.T,
	configs config.ComponentConfigs,
) (*ComponentManager, *[]*rulePackInitProbe, *int) {
	t.Helper()
	registry := component.NewRegistry()
	probes := make([]*rulePackInitProbe, 0)
	ruleFactoryCalls := 0
	ruleFactory := func(
		raw json.RawMessage,
		_ component.Dependencies,
	) (component.Discoverable, error) {
		ruleFactoryCalls++
		var fixture rulePackInitFixture
		if err := json.Unmarshal(raw, &fixture); err != nil {
			return nil, err
		}
		if fixture.FactoryError != "" {
			return nil, errors.New(fixture.FactoryError)
		}
		probe := &rulePackInitProbe{
			baseDiscoverable: baseDiscoverable{name: "rule-processor"},
		}
		if fixture.InitializeError != "" {
			probe.initializeErr = errors.New(fixture.InitializeError)
		}
		probes = append(probes, probe)
		return probe, nil
	}
	require.NoError(t, registry.RegisterFactory("rule-processor", &component.Registration{
		Name: "rule-processor", Type: "processor", Factory: ruleFactory,
	}))
	require.NoError(t, registry.RegisterFactory("ordinary-failing", &component.Registration{
		Name: "ordinary-failing", Type: "processor",
		Factory: func(json.RawMessage, component.Dependencies) (component.Discoverable, error) {
			return nil, errors.New("ordinary component failure")
		},
	}))
	return &ComponentManager{
		BaseService:      NewBaseServiceWithOptions("component-manager", nil),
		registry:         registry,
		natsClient:       new(natsclient.Client),
		componentConfigs: configs,
		components:       make(map[string]*component.ManagedComponent),
		resources:        make(map[string][]string),
	}, &probes, &ruleFactoryCalls
}

func TestComponentManagerInitializeAggregatesEnabledRulePackFailuresDeterministically(t *testing.T) {
	t.Parallel()
	manager, probes, ruleFactoryCalls := newRulePackColdBootManager(t, config.ComponentConfigs{
		"zeta-pack": {
			Type: types.ComponentTypeProcessor, Name: "rule-processor", Enabled: true,
			Config: json.RawMessage(`{"initialize_error":"zeta init failed"}`),
		},
		"alpha-pack": {
			Type: types.ComponentTypeProcessor, Name: "rule-processor", Enabled: true,
			Config: json.RawMessage(`{"factory_error":"alpha factory failed"}`),
		},
		"valid-pack": {
			Type: types.ComponentTypeProcessor, Name: "rule-processor", Enabled: true,
			Config: json.RawMessage(`{}`),
		},
		"disabled-invalid-pack": {
			Type: types.ComponentTypeProcessor, Name: "rule-processor", Enabled: false,
			Config: json.RawMessage(`{"factory_error":"must be ignored"}`),
		},
		"ordinary-failure": {
			Type: types.ComponentTypeProcessor, Name: "ordinary-failing", Enabled: true,
		},
	})

	err := manager.Initialize()
	require.Error(t, err)
	require.ErrorContains(t, err, "alpha-pack")
	require.ErrorContains(t, err, "alpha factory failed")
	require.ErrorContains(t, err, "zeta-pack")
	require.ErrorContains(t, err, "zeta init failed")
	require.Less(t,
		strings.Index(err.Error(), "alpha-pack"),
		strings.Index(err.Error(), "zeta-pack"),
		err.Error(),
	)
	require.Equal(t, 3, *ruleFactoryCalls, "disabled rule pack must not call its factory")
	require.Contains(t, manager.components, "valid-pack",
		"creation pass must continue after a rule-pack failure")
	require.NotContains(t, manager.components, "ordinary-failure")
	require.False(t, manager.initialized.Load())
	for _, probe := range *probes {
		require.Zero(t, probe.startCalls)
	}
}

func TestComponentManagerInitializeOrdinaryFailureRemainsBestEffort(t *testing.T) {
	t.Parallel()
	manager, _, _ := newRulePackColdBootManager(t, config.ComponentConfigs{
		"ordinary-failure": {
			Type: types.ComponentTypeProcessor, Name: "ordinary-failing", Enabled: true,
		},
	})

	require.NoError(t, manager.Initialize())
	require.True(t, manager.initialized.Load())
	require.Empty(t, manager.components)
}
