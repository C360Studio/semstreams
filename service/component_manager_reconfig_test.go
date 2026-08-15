// Package service — gh#455 regression.
//
// The ComponentManager PUT config handler must hot-apply a component that
// implements the service-flavored reconfig method pair
// (ValidateConfigUpdate/ApplyConfigUpdate, e.g. processor/rule), not only the
// component-side UpdateConfig contract — and it must report honestly whether the
// change was applied live vs stored for restart. Before the fix, the handler
// silently no-op'd the method-pair components and returned unconditional success.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/internal/lifecyclejoin"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock components ---

// baseDiscoverable supplies the component.Discoverable surface; variants embed
// it and add whichever reconfig contract they implement (or none).
type baseDiscoverable struct{ name string }

func (b baseDiscoverable) Meta() component.Metadata {
	return component.Metadata{Name: b.name, Type: "processor"}
}
func (b baseDiscoverable) InputPorts() []component.Port         { return nil }
func (b baseDiscoverable) OutputPorts() []component.Port        { return nil }
func (b baseDiscoverable) ConfigSchema() component.ConfigSchema { return component.ConfigSchema{} }
func (b baseDiscoverable) Health() component.HealthStatus {
	return component.HealthStatus{Healthy: true}
}
func (b baseDiscoverable) DataFlow() component.FlowMetrics { return component.FlowMetrics{} }

// reconfigPairComponent implements the service-flavored method pair (the
// processor/rule shape) and records what it was asked to validate/apply.
type reconfigPairComponent struct {
	baseDiscoverable
	validateErr error
	validated   []map[string]any
	applied     []map[string]any
}

func (c *reconfigPairComponent) ValidateConfigUpdate(changes map[string]any) error {
	c.validated = append(c.validated, changes)
	return c.validateErr
}
func (c *reconfigPairComponent) ApplyConfigUpdate(changes map[string]any) error {
	c.applied = append(c.applied, changes)
	return nil
}

// updateConfigComponent implements the component-side UpdateConfig contract.
type updateConfigComponent struct {
	baseDiscoverable
	calls []json.RawMessage
}

func (c *updateConfigComponent) UpdateConfig(_ context.Context, cfg json.RawMessage) error {
	c.calls = append(c.calls, cfg)
	return nil
}

// bothContractsComponent implements BOTH contracts; the bridge must prefer
// UpdateConfig and never fall through to the method pair.
type bothContractsComponent struct {
	baseDiscoverable
	updateCalls int
	applyCalls  int
}

type declarationConfigComponent struct {
	baseDiscoverable
	subject     string
	updateCalls int
}

func (c *declarationConfigComponent) OutputPorts() []component.Port {
	return []component.Port{{
		Name: "events", Direction: component.DirectionOutput,
		Config: component.NATSPort{Subject: c.subject},
	}}
}

func (c *declarationConfigComponent) UpdateConfig(context.Context, json.RawMessage) error {
	c.updateCalls++
	return nil
}

func (c *bothContractsComponent) UpdateConfig(_ context.Context, _ json.RawMessage) error {
	c.updateCalls++
	return nil
}
func (c *bothContractsComponent) ValidateConfigUpdate(_ map[string]any) error { return nil }
func (c *bothContractsComponent) ApplyConfigUpdate(_ map[string]any) error {
	c.applyCalls++
	return nil
}

// noHookComponent implements no runtime-reconfig contract.
type noHookComponent struct{ baseDiscoverable }

type blockingUpdateComponent struct {
	baseDiscoverable
	applyEntered chan struct{}
	releaseApply chan struct{}
	applyCalls   int
}

func (c *blockingUpdateComponent) UpdateConfig(context.Context, json.RawMessage) error {
	c.applyCalls++
	close(c.applyEntered)
	<-c.releaseApply
	return nil
}

type lifecycleGenerationProbe struct {
	baseDiscoverable
	initializeCalls int
	startCalls      int
	stopCalls       int
	stopErr         error
}

func (c *lifecycleGenerationProbe) Initialize() error {
	c.initializeCalls++
	return nil
}
func (c *lifecycleGenerationProbe) Start(context.Context) error {
	c.startCalls++
	return nil
}
func (c *lifecycleGenerationProbe) Stop(context.Context) error {
	c.stopCalls++
	return c.stopErr
}

// --- applyRuntimeConfig unit tests (the bridge core) ---

func TestApplyRuntimeConfig_MethodPairBridged(t *testing.T) {
	cm := &ComponentManager{}
	comp := &reconfigPairComponent{baseDiscoverable: baseDiscoverable{name: "rule"}}

	applied, err := cm.applyRuntimeConfig(context.Background(), comp,
		json.RawMessage(`{"enable_graph_integration":false}`))

	require.NoError(t, err)
	assert.True(t, applied, "method-pair component must be applied live")
	require.Len(t, comp.validated, 1, "ValidateConfigUpdate must be called")
	require.Len(t, comp.applied, 1, "ApplyConfigUpdate must be called")
	assert.Equal(t, false, comp.applied[0]["enable_graph_integration"])
}

func TestApplyRuntimeConfig_UpdateConfigPreferredOverPair(t *testing.T) {
	cm := &ComponentManager{}
	comp := &bothContractsComponent{baseDiscoverable: baseDiscoverable{name: "both"}}

	applied, err := cm.applyRuntimeConfig(context.Background(), comp, json.RawMessage(`{}`))

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Equal(t, 1, comp.updateCalls, "UpdateConfig must be probed first")
	assert.Equal(t, 0, comp.applyCalls, "the method pair must NOT run when UpdateConfig exists")
}

func TestApplyRuntimeConfig_UpdateConfigPath(t *testing.T) {
	cm := &ComponentManager{}
	comp := &updateConfigComponent{baseDiscoverable: baseDiscoverable{name: "uc"}}

	applied, err := cm.applyRuntimeConfig(context.Background(), comp, json.RawMessage(`{"k":"v"}`))

	require.NoError(t, err)
	assert.True(t, applied)
	require.Len(t, comp.calls, 1, "UpdateConfig must receive the raw config verbatim")
	assert.JSONEq(t, `{"k":"v"}`, string(comp.calls[0]))
}

func TestApplyRuntimeConfig_NoHookNotApplied(t *testing.T) {
	cm := &ComponentManager{}
	comp := &noHookComponent{baseDiscoverable{name: "nohook"}}

	applied, err := cm.applyRuntimeConfig(context.Background(), comp, json.RawMessage(`{}`))

	require.NoError(t, err, "a component with no reconfig hook is not an error")
	assert.False(t, applied, "no hook → not applied (caller reports applied:false)")
}

func TestApplyRuntimeConfig_ValidationRejectionWrapsSentinelAndSkipsApply(t *testing.T) {
	cm := &ComponentManager{}
	comp := &reconfigPairComponent{
		baseDiscoverable: baseDiscoverable{name: "rule"},
		validateErr:      assert.AnError,
	}

	applied, err := cm.applyRuntimeConfig(context.Background(), comp, json.RawMessage(`{"x":1}`))

	require.Error(t, err)
	assert.ErrorIs(t, err, errReconfigValidation, "validation failure must wrap the 400 sentinel")
	assert.False(t, applied)
	assert.Empty(t, comp.applied, "ApplyConfigUpdate must NOT run after a validation failure")
}

// --- handler tests (httptest through the production wire) ---

// newReconfigTestCM builds a minimal ComponentManager wired for the PUT config
// handler: one component + its stored ComponentConfig, configManager nil so the
// schema-validation step is skipped and the reconfig path is exercised directly.
func newReconfigTestCM(name string, comp component.Discoverable, storedConfig json.RawMessage) *ComponentManager {
	effectiveConfig := types.ComponentConfig{Type: "processor", Name: name, Enabled: true, Config: storedConfig}
	registry := component.NewRegistry()
	requireNoErrorForReconfigSetup(registry.RegisterWithConfig(component.RegistrationConfig{
		Name: name, Type: "processor",
		Factory: func(json.RawMessage, component.Dependencies) (component.Discoverable, error) { return comp, nil },
	}))
	client := new(natsclient.Client)
	requireNoErrorForReconfigSetupValue(registry.CreateComponent(
		name, effectiveConfig, component.Dependencies{NATSClient: client}))
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		// Mirror production: the managed component retains its effective config
		// (populated by CreateComponent) so the gh#520 guard baseline is realistic.
		components: map[string]*component.ManagedComponent{
			name: {Component: comp, State: component.StateStarted, Config: effectiveConfig},
		},
		componentConfigs: config.ComponentConfigs{name: effectiveConfig},
		registry:         registry,
		natsClient:       client,
	}
	return cm
}

func requireNoErrorForReconfigSetup(err error) {
	if err != nil {
		panic(err)
	}
}

func requireNoErrorForReconfigSetupValue(_ component.Discoverable, err error) {
	requireNoErrorForReconfigSetup(err)
}

func putConfig(cm *ComponentManager, name string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/components/config/"+name, strings.NewReader(body))
	w := httptest.NewRecorder()
	cm.handlePutComponentConfig(w, req)
	return w
}

func getConfig(cm *ComponentManager, name string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/components/config/"+name, nil)
	w := httptest.NewRecorder()
	cm.handleGetComponentConfig(w, req)
	return w
}

func TestHandlePutComponentConfigMissingNamesUseBoundedOperationStripes(t *testing.T) {
	cm := &ComponentManager{}
	seenStripes := make(map[uint64]struct{})
	for index := 0; index < 2048; index++ {
		name := fmt.Sprintf("missing-%d", index)
		response := putConfig(cm, name, `{"config":{}}`)
		require.Equal(t, http.StatusNotFound, response.Code)
		seenStripes[componentOperationStripe(name)] = struct{}{}
	}
	if got := len(cm.instanceOps); got != componentOperationStripeCount {
		t.Fatalf("operation stripe count = %d, want fixed bound %d", got, componentOperationStripeCount)
	}
	assert.LessOrEqual(t, len(seenStripes), componentOperationStripeCount)
	assert.Equal(t, componentOperationStripe("same-name"), componentOperationStripe("same-name"),
		"the same identity must always select the same sequencing stripe")
}

// TestHandleGetComponentConfig_ReflectsEffectiveConfigNotStaleBaseline is the
// gh#522 regression: GET /config must return the effective config the component is
// running (ManagedComponent.Config, the single source of truth refreshed on every
// write path), NOT the boot-time componentConfigs baseline that is left stale by a
// KV-watch-driven restart. We reproduce the stale condition directly: refresh only
// mc.Config (as CreateComponent does on a KV restart) while componentConfigs still
// holds the old body, and assert GET returns the new body.
func TestHandleGetComponentConfig_ReflectsEffectiveConfigNotStaleBaseline(t *testing.T) {
	comp := &reconfigPairComponent{baseDiscoverable: baseDiscoverable{name: "rule-processor"}}
	cm := newReconfigTestCM("rule-processor", comp, json.RawMessage(`{"old":true}`))

	// Simulate a KV-watch restart: only mc.Config is refreshed (componentConfigs
	// stays stale, exactly the gh#522 bug condition).
	cm.mu.Lock()
	mc := cm.components["rule-processor"]
	mc.Config.Config = json.RawMessage(`{"new":true}`)
	cm.mu.Unlock()

	w := getConfig(cm, "rule-processor")
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	body, _ := json.Marshal(resp["config"])
	assert.JSONEq(t, `{"new":true}`, string(body),
		"GET /config must reflect the effective config, not the stale componentConfigs baseline")
}

func TestHandlePutComponentConfig_MethodPairAppliesAndReportsApplied(t *testing.T) {
	comp := &reconfigPairComponent{baseDiscoverable: baseDiscoverable{name: "rule-processor"}}
	cm := newReconfigTestCM("rule-processor", comp, json.RawMessage(`{"old":true}`))
	before := cm.registry.Component("rule-processor")

	w := putConfig(cm, "rule-processor", `{"config":{"enable_graph_integration":false}}`)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["applied"], "bridge must report applied=true")
	// Reconfig-observable: the component's ApplyConfigUpdate actually ran.
	require.Len(t, comp.applied, 1)
	// The effective config (the single source of truth, gh#522) was updated.
	cm.mu.Lock()
	effective := cm.components["rule-processor"].Config.Config
	cm.mu.Unlock()
	assert.JSONEq(t, `{"enable_graph_integration":false}`, string(effective))
	assert.Same(t, before, cm.registry.Component("rule-processor"),
		"declaration-neutral live update must retain its admitted component")
}

func TestHandlePutComponentConfig_NoHookReportsNotApplied(t *testing.T) {
	comp := &noHookComponent{baseDiscoverable{name: "static"}}
	cm := newReconfigTestCM("static", comp, json.RawMessage(`{"old":true}`))

	w := putConfig(cm, "static", `{"config":{"a":1}}`)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, false, resp["applied"], "no-hook component must not claim a live apply")
	// Must NOT promise a restart-time apply — this endpoint does not persist
	// durably (gh#388), so a restart would revert the change (gh#455 review HIGH).
	assert.NotContains(t, resp, "restart_required", "must not promise a restart-time apply the endpoint can't keep")
}

func TestHandlePutComponentConfig_DeclarationChangeRefusesBeforeMutation(t *testing.T) {
	registry := component.NewRegistry()
	live := &declarationConfigComponent{
		baseDiscoverable: baseDiscoverable{name: "declared"}, subject: "events.old",
	}
	require.NoError(t, registry.RegisterWithConfig(component.RegistrationConfig{
		Name: "declared", Type: "processor",
		Factory: func(raw json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
			var cfg struct {
				Subject string `json:"subject"`
			}
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, err
			}
			if cfg.Subject == "events.old" {
				return live, nil
			}
			return &declarationConfigComponent{
				baseDiscoverable: baseDiscoverable{name: "declared"}, subject: cfg.Subject,
			}, nil
		},
	}))
	client := new(natsclient.Client)
	stored := json.RawMessage(`{"subject":"events.old"}`)
	effective := types.ComponentConfig{
		Name: "declared", Type: types.ComponentTypeProcessor, Enabled: true, Config: stored,
	}
	_, err := registry.CreateComponent(
		"declared", effective, component.Dependencies{NATSClient: client})
	require.NoError(t, err)
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		registry:    registry,
		natsClient:  client,
		components: map[string]*component.ManagedComponent{
			"declared": {Component: live, State: component.StateStarted, Config: effective},
		},
	}

	w := putConfig(cm, "declared", `{"config":{"subject":"events.new"}}`)
	require.Equal(t, http.StatusConflict, w.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.Equal(t, "declaration_change_requires_replacement", response["code"])
	assert.Zero(t, live.updateCalls, "live component mutated before declaration refusal")
	assert.JSONEq(t, `{"subject":"events.old"}`, string(cm.components["declared"].Config.Config))
	assert.Same(t, live, registry.Component("declared"))
}

func TestHandlePutComponentConfigSerializesProofApplyAndCommitWithReplacement(t *testing.T) {
	registry := component.NewRegistry()
	proofEntered := make(chan struct{})
	releaseProof := make(chan struct{})
	replacementFactoryEntered := make(chan struct{})
	live := &blockingUpdateComponent{
		baseDiscoverable: baseDiscoverable{name: "sequenced"},
		applyEntered:     make(chan struct{}),
		releaseApply:     make(chan struct{}),
	}
	replacement := &noHookComponent{baseDiscoverable{name: "sequenced"}}
	require.NoError(t, registry.RegisterWithConfig(component.RegistrationConfig{
		Name: "sequenced", Type: "processor",
		Factory: func(raw json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
			var cfg struct {
				Mode string `json:"mode"`
			}
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, err
			}
			switch cfg.Mode {
			case "initial":
				return live, nil
			case "put":
				close(proofEntered)
				<-releaseProof
				return &noHookComponent{baseDiscoverable{name: "sequenced"}}, nil
			case "replacement":
				close(replacementFactoryEntered)
				return replacement, nil
			default:
				return nil, errors.New("unknown mode")
			}
		},
	}))
	client := new(natsclient.Client)
	initialCfg := types.ComponentConfig{
		Name: "sequenced", Type: types.ComponentTypeProcessor, Enabled: true,
		Config: json.RawMessage(`{"mode":"initial"}`),
	}
	_, err := registry.CreateComponent(
		"sequenced", initialCfg, component.Dependencies{NATSClient: client})
	require.NoError(t, err)
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		registry:    registry, natsClient: client,
		components: map[string]*component.ManagedComponent{
			"sequenced": {Component: live, State: component.StateStarted, Config: initialCfg},
		},
	}

	putDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		putDone <- putConfig(cm, "sequenced", `{"config":{"mode":"put"}}`)
	}()
	<-proofEntered
	operationMu := &cm.instanceOps[componentOperationStripe("sequenced")]
	assert.False(t, operationMu.TryLock(), "PUT must hold the instance operation lock during declaration proof")

	replaceStarted := make(chan struct{})
	replaceDone := make(chan error, 1)
	stale := cm.components["sequenced"]
	replacementCfg := initialCfg
	replacementCfg.Config = json.RawMessage(`{"mode":"replacement"}`)
	go func() {
		close(replaceStarted)
		replaceDone <- cm.recreateComponentWithNewConfig(context.Background(), "sequenced", replacementCfg, stale)
	}()
	<-replaceStarted
	close(releaseProof)
	<-live.applyEntered
	assert.False(t, operationMu.TryLock(), "PUT must retain sequencing through live mutation and config commit")
	select {
	case <-replacementFactoryEntered:
		t.Fatal("replacement factory ran before live PUT committed retained config")
	default:
	}
	close(live.releaseApply)
	response := <-putDone
	require.Equal(t, http.StatusOK, response.Code)
	<-replacementFactoryEntered
	require.NoError(t, <-replaceDone)
	assert.Equal(t, 1, live.applyCalls)
	assert.Same(t, replacement, cm.components["sequenced"].Component)
	assert.JSONEq(t, `{"mode":"replacement"}`, string(cm.components["sequenced"].Config.Config))
}

func TestRecreateUsesAuthoritativeComponentAfterWaitingOnInstanceSequence(t *testing.T) {
	cm, old, middle, final, oldManaged, configs := newStaleGenerationTestManager(t)
	require.NoError(t, cm.recreateComponentWithNewConfig(
		context.Background(), "sequenced", configs["middle"], oldManaged))
	require.NoError(t, cm.recreateComponentWithNewConfig(
		context.Background(), "sequenced", configs["final"], oldManaged))
	assert.Equal(t, 1, old.stopCalls)
	assert.Equal(t, 0, middle.stopCalls, "a replacement generation whose Start was never invoked must not be stopped")
	assert.Equal(t, 0, final.stopCalls)
	assert.Same(t, final, cm.registry.Component("sequenced"))
}

func TestRemovalUsesAuthoritativeComponentAfterWaitingOnInstanceSequence(t *testing.T) {
	cm, old, middle, _, oldManaged, configs := newStaleGenerationTestManager(t)
	require.NoError(t, cm.recreateComponentWithNewConfig(
		context.Background(), "sequenced", configs["middle"], oldManaged))
	require.NoError(t, cm.stopAndRemoveComponent(context.Background(), "sequenced", oldManaged))
	assert.Equal(t, 1, old.stopCalls)
	assert.Equal(t, 0, middle.stopCalls, "removal must not Stop the current never-started generation")
	assert.Nil(t, cm.registry.Component("sequenced"))
	assert.NotContains(t, cm.components, "sequenced")
}

func TestReplacementOldStopFailureAbortsCandidateAndRetainsOldGenerationDegraded(t *testing.T) {
	cm, old, replacement, _, oldManaged, configs := newStaleGenerationTestManager(t)
	stopErr := errors.New("old generation still draining")
	old.stopErr = stopErr

	err := cm.restartComponentWithNewConfig(
		context.Background(), "sequenced", configs["middle"], oldManaged)
	var retirementErr *replacementRetirementError
	require.ErrorAs(t, err, &retirementErr)
	require.ErrorIs(t, err, stopErr)
	assert.Equal(t, "replacement_aborted_old_retirement_failed", restartFailureAction(err))
	assert.Equal(t, 0, replacement.startCalls, "replacement candidate must not Start while old retirement is unresolved")
	assert.Equal(t, 0, replacement.stopCalls, "aborted candidate whose Start was never invoked must not be stopped")
	assert.Same(t, old, cm.registry.Component("sequenced"))
	managed := cm.components["sequenced"]
	assert.Same(t, old, managed.Component)
	assert.Equal(t, component.StateFailed, managed.State)
	assert.ErrorIs(t, managed.LastError, stopErr)
}

func TestRemovalStopFailureLeavesComponentAndRegistryAdmitted(t *testing.T) {
	cm, old, _, _, oldManaged, _ := newStaleGenerationTestManager(t)
	stopErr := errors.New("component still owns listener")
	old.stopErr = stopErr

	err := cm.stopAndRemoveComponent(context.Background(), "sequenced", oldManaged)
	require.ErrorIs(t, err, stopErr)
	assert.Same(t, old, cm.registry.Component("sequenced"))
	managed := cm.components["sequenced"]
	assert.Same(t, oldManaged, managed)
	assert.Equal(t, component.StateFailed, managed.State)
	assert.ErrorIs(t, managed.LastError, stopErr)
}

func newStaleGenerationTestManager(t *testing.T) (
	*ComponentManager,
	*lifecycleGenerationProbe,
	*lifecycleGenerationProbe,
	*lifecycleGenerationProbe,
	*component.ManagedComponent,
	map[string]types.ComponentConfig,
) {
	t.Helper()
	registry := component.NewRegistry()
	probes := map[string]*lifecycleGenerationProbe{
		"old":    {baseDiscoverable: baseDiscoverable{name: "sequenced"}},
		"middle": {baseDiscoverable: baseDiscoverable{name: "sequenced"}},
		"final":  {baseDiscoverable: baseDiscoverable{name: "sequenced"}},
	}
	require.NoError(t, registry.RegisterWithConfig(component.RegistrationConfig{
		Name: "sequenced", Type: "processor",
		Factory: func(raw json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
			var cfg struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, err
			}
			return probes[cfg.ID], nil
		},
	}))
	configs := make(map[string]types.ComponentConfig, len(probes))
	for id := range probes {
		configs[id] = types.ComponentConfig{
			Name: "sequenced", Type: types.ComponentTypeProcessor, Enabled: true,
			Config: json.RawMessage(fmt.Sprintf(`{"id":%q}`, id)),
		}
	}
	client := new(natsclient.Client)
	_, err := registry.CreateComponent(
		"sequenced", configs["old"], component.Dependencies{NATSClient: client})
	require.NoError(t, err)
	oldManaged := &component.ManagedComponent{
		Component: probes["old"], State: component.StateStarted, Config: configs["old"],
	}
	startDone := make(chan struct{})
	close(startDone)
	_, cancel := context.WithCancel(context.Background())
	runtime := &componentRuntime{startDone: startDone, startInvoked: true}
	runtime.generation = lifecyclejoin.NewGeneration(cancel, func() { <-startDone })
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		registry:    registry, natsClient: client,
		components: make(map[string]*component.ManagedComponent),
		runtimes:   make(map[string]*componentRuntime),
	}
	startPostBootComponentManager(t, cm)
	cm.mu.Lock()
	cm.components["sequenced"] = oldManaged
	cm.runtimes["sequenced"] = runtime
	cm.mu.Unlock()
	return cm, probes["old"], probes["middle"], probes["final"], oldManaged, configs
}

// TestHandlePutComponentConfig_RefreshesGuardBaseline locks the gh#520 finding-#1
// fix: a live PUT reconfig must refresh the retained ManagedComponent.Config
// baseline that the KV-watch idempotency guard compares against. Otherwise a later
// KV re-push of the same config would spuriously restart (stale baseline != push)
// or silently skip reconverging durable desired state.
func TestHandlePutComponentConfig_RefreshesGuardBaseline(t *testing.T) {
	comp := &reconfigPairComponent{baseDiscoverable: baseDiscoverable{name: "rule-processor"}}
	cm := newReconfigTestCM("rule-processor", comp, json.RawMessage(`{"old":true}`))

	w := putConfig(cm, "rule-processor", `{"config":{"enable_graph_integration":false}}`)
	require.Equal(t, http.StatusOK, w.Code)

	// The retained baseline on the managed component (not just componentConfigs)
	// now reflects the live-applied config.
	cm.mu.Lock()
	baseline := cm.components["rule-processor"].Config.Config
	cm.mu.Unlock()
	assert.JSONEq(t, `{"enable_graph_integration":false}`, string(baseline),
		"live PUT must refresh ManagedComponent.Config so the KV-watch guard baseline is not stale")

	// And a subsequent KV-watch update carrying that same effective config is a
	// no-op (Equal against the refreshed baseline), not a spurious restart.
	updated := types.ComponentConfig{Type: "processor", Name: "rule-processor", Enabled: true, Config: json.RawMessage(`{"enable_graph_integration":false}`)}
	cm.mu.Lock()
	same := cm.components["rule-processor"].Config.Equal(updated)
	cm.mu.Unlock()
	assert.True(t, same, "refreshed baseline must compare equal to the live-applied config")
}

func TestHandlePutComponentConfig_ValidationRejectionReturns400AndDoesNotStore(t *testing.T) {
	comp := &reconfigPairComponent{
		baseDiscoverable: baseDiscoverable{name: "rule-processor"},
		validateErr:      assert.AnError,
	}
	original := json.RawMessage(`{"old":true}`)
	cm := newReconfigTestCM("rule-processor", comp, original)

	w := putConfig(cm, "rule-processor", `{"config":{"enable_graph_integration":false}}`)

	require.Equal(t, http.StatusBadRequest, w.Code, "component validation rejection → structured 400")
	assert.Empty(t, comp.applied, "nothing applied on validation failure")
	// The effective config MUST be unchanged — apply runs before the in-memory
	// refresh, so a rejected update leaves the source of truth untouched (gh#522).
	cm.mu.Lock()
	effective := cm.components["rule-processor"].Config.Config
	cm.mu.Unlock()
	assert.JSONEq(t, `{"old":true}`, string(effective))
}
