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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
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
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		components:  map[string]*component.ManagedComponent{name: {Component: comp, State: component.StateStarted}},
		componentConfigs: config.ComponentConfigs{
			name: types.ComponentConfig{Type: "processor", Name: name, Enabled: true, Config: storedConfig},
		},
		registry: component.NewRegistry(),
	}
	return cm
}

func putConfig(cm *ComponentManager, name string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/components/config/"+name, strings.NewReader(body))
	w := httptest.NewRecorder()
	cm.handlePutComponentConfig(w, req)
	return w
}

func TestHandlePutComponentConfig_MethodPairAppliesAndReportsApplied(t *testing.T) {
	comp := &reconfigPairComponent{baseDiscoverable: baseDiscoverable{name: "rule-processor"}}
	cm := newReconfigTestCM("rule-processor", comp, json.RawMessage(`{"old":true}`))

	w := putConfig(cm, "rule-processor", `{"config":{"enable_graph_integration":false}}`)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["applied"], "bridge must report applied=true")
	// Reconfig-observable: the component's ApplyConfigUpdate actually ran.
	require.Len(t, comp.applied, 1)
	// Stored config was updated to the new value.
	assert.JSONEq(t, `{"enable_graph_integration":false}`, string(cm.componentConfigs["rule-processor"].Config))
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
	// The stored config MUST be unchanged — a restart must not load the rejected update.
	assert.JSONEq(t, `{"old":true}`, string(cm.componentConfigs["rule-processor"].Config))
}
