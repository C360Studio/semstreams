package flowstore

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/types"
)

func TestDecorateComparesDesiredAgainstSealedBootSnapshot(t *testing.T) {
	boot := config.ComponentConfigs{
		"worker": {
			Name: "first", Type: types.ComponentTypeProcessor, Enabled: true, Config: json.RawMessage(`{"value":1}`),
		},
	}
	desired := cloneComponentConfigs(boot)
	manager := &Manager{
		bootID: "boot-a", bootConfig: cloneComponentConfigs(boot),
		desired: func() config.ComponentConfigs { return cloneComponentConfigs(desired) },
	}
	flow := &Flow{
		ID: "flow-a", Name: "Flow A", DesiredState: DesiredEnabled,
		Nodes: []FlowNode{{ID: "worker", Name: "worker", Component: "first", Type: types.ComponentTypeProcessor}},
	}

	manager.decorate(flow)
	if flow.EffectiveState != EffectiveUnknown {
		t.Fatalf("effective state = %q, want unknown without observer", flow.EffectiveState)
	}
	if flow.RestartRequired {
		t.Fatal("identical desired and boot configuration requires restart")
	}
	if flow.DesiredProvenance == nil {
		t.Fatal("decorated flow is missing desired provenance")
	}
	if flow.DesiredProvenance.BootID != "" {
		t.Fatalf("desired provenance claimed a boot identity: %#v", flow.DesiredProvenance)
	}
	if flow.BootAppliedProvenance != nil {
		t.Fatalf("boot-applied provenance without an observer = %#v, want unknown", flow.BootAppliedProvenance)
	}

	changed := desired["worker"]
	changed.Config = json.RawMessage(`{"value":2}`)
	desired["worker"] = changed
	manager.decorate(flow)
	if !flow.RestartRequired {
		t.Fatal("post-boot desired change did not require restart")
	}
	if got := boot["worker"].Config; string(got) != `{"value":1}` {
		t.Fatalf("desired change mutated sealed boot snapshot: %s", got)
	}
}

func TestDecorateCanonicalizesComponentConfigJSON(t *testing.T) {
	boot := config.ComponentConfigs{
		"worker": {
			Name: "first", Type: types.ComponentTypeProcessor, Enabled: true,
			Config: json.RawMessage(`{"alpha":1,"beta":{"x":2,"y":3}}`),
		},
	}
	desired := cloneComponentConfigs(boot)
	changed := desired["worker"]
	changed.Config = json.RawMessage("{\n  \"beta\": {\"y\": 3, \"x\": 2},\n  \"alpha\": 1\n}")
	desired["worker"] = changed
	manager := &Manager{
		bootID: "boot-a", bootConfig: cloneComponentConfigs(boot),
		desired: func() config.ComponentConfigs { return cloneComponentConfigs(desired) },
	}
	flow := &Flow{
		ID: "flow-a", Name: "Flow A", DesiredState: DesiredEnabled,
		Nodes: []FlowNode{{ID: "worker", Name: "worker", Component: "first", Type: types.ComponentTypeProcessor}},
	}

	manager.decorate(flow)
	if flow.RestartRequired {
		t.Fatal("equivalent JSON formatting and key order required a restart")
	}
}

func TestMarshalPersistedFlowOmitsRuntimeObservation(t *testing.T) {
	flow := &Flow{
		ID: "flow-a", Name: "Flow A", DesiredState: DesiredEnabled,
		EffectiveState: EffectiveEnabled, RestartRequired: true,
		DesiredProvenance:     &ConfigProvenance{BootID: "boot-a", Digest: "desired"},
		BootAppliedProvenance: &ConfigProvenance{BootID: "boot-a", Digest: "boot"},
	}
	data, err := marshalPersistedFlow(flow)
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"effective_state", "restart_required", "desired_provenance", "boot_applied_provenance"} {
		if _, ok := persisted[field]; ok {
			t.Fatalf("persisted flow contains transient field %q: %s", field, data)
		}
	}
}

func TestBootIDsAreUnique(t *testing.T) {
	if first, second := newBootID(), newBootID(); first == second {
		t.Fatalf("boot IDs are not unique: %q", first)
	}
}
