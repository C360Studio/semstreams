package flowstore

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/types"
)

func componentSet(enabled bool, raw string) DesiredComponentSet {
	return DesiredComponentSet{"worker": {
		Name: "first", Type: types.ComponentTypeProcessor, Enabled: enabled, Config: json.RawMessage(raw),
	}}
}

func boolPointer(t *testing.T, got *bool, want bool) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("restart_required = %v, want %t", got, want)
	}
}

func TestSelectBootTruthTable(t *testing.T) {
	bootFlows := []*Flow{
		{ID: "absent", Name: "Absent", DesiredState: DesiredAbsent},
		{ID: "disabled", Name: "Disabled", DesiredState: DesiredDisabled, DesiredComponents: componentSet(false, `{"value":1}`)},
		{ID: "enabled", Name: "Enabled", DesiredState: DesiredEnabled, DesiredComponents: DesiredComponentSet{"other": {
			Name: "first", Type: types.ComponentTypeProcessor, Enabled: true, Config: json.RawMessage(`{"value":2}`),
		}}},
	}
	selection, err := SelectBoot(&config.Config{Components: config.ComponentConfigs{}}, bootFlows)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		flow      *Flow
		effective EffectiveState
		restart   bool
	}{
		{"absent_equal", &Flow{ID: "absent", DesiredState: DesiredAbsent}, EffectiveAbsent, false},
		{"absent_now_present", &Flow{ID: "absent", DesiredState: DesiredDisabled, DesiredComponents: componentSet(false, `{"value":1}`)}, EffectiveAbsent, true},
		{"present_now_absent", &Flow{ID: "disabled", DesiredState: DesiredAbsent}, EffectiveDisabled, true},
		{"exact_equal", &Flow{ID: "disabled", DesiredState: DesiredDisabled, DesiredComponents: componentSet(false, `{"value":1}`)}, EffectiveDisabled, false},
		{"config_diff", &Flow{ID: "disabled", DesiredState: DesiredDisabled, DesiredComponents: componentSet(false, `{"value":9}`)}, EffectiveDisabled, true},
		{"enabled_diff", &Flow{ID: "disabled", DesiredState: DesiredEnabled, DesiredComponents: componentSet(true, `{"value":1}`)}, EffectiveDisabled, true},
		{"membership_diff", &Flow{ID: "disabled", DesiredState: DesiredDisabled, DesiredComponents: DesiredComponentSet{"renamed": {
			Name: "first", Type: types.ComponentTypeProcessor, Enabled: false, Config: json.RawMessage(`{"value":1}`),
		}}}, EffectiveDisabled, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observation := selection.Observe(tt.flow)
			if observation.EffectiveState != tt.effective {
				t.Fatalf("effective state = %q, want %q", observation.EffectiveState, tt.effective)
			}
			boolPointer(t, observation.RestartRequired, tt.restart)
			if observation.BootAppliedProvenance == nil || observation.BootAppliedProvenance.BootID == "" {
				t.Fatalf("missing boot provenance: %#v", observation.BootAppliedProvenance)
			}
			if observation.DesiredProvenance == nil {
				t.Fatal("missing desired provenance")
			}
		})
	}
}

func TestUnavailableBootSelectionIsUnknown(t *testing.T) {
	observation := (*BootSelection)(nil).Observe(&Flow{ID: "flow-a", DesiredState: DesiredAbsent})
	if observation.EffectiveState != EffectiveUnknown || observation.RestartRequired != nil {
		t.Fatalf("unavailable observation = %#v", observation)
	}
}

func TestSelectBootCombinesStaticAndFlowComponentsWithoutAliasing(t *testing.T) {
	effective := &config.Config{Components: config.ComponentConfigs{"static-worker": {
		Name: "first", Type: types.ComponentTypeProcessor, Enabled: true, Config: json.RawMessage(`{"static":true}`),
	}}}
	flow := &Flow{ID: "flow-a", DesiredState: DesiredEnabled, DesiredComponents: componentSet(true, `{"value":1}`)}
	selection, err := SelectBoot(effective, []*Flow{flow})
	if err != nil {
		t.Fatal(err)
	}
	selected := selection.Config()
	if len(selected.Components) != 2 {
		t.Fatalf("selected components = %#v", selected.Components)
	}
	selected.Components["worker"] = types.ComponentConfig{}
	if got := selection.Config().Components["worker"].Name; got != "first" {
		t.Fatalf("Config returned aliased state: %q", got)
	}
	flow.DesiredComponents["worker"] = types.ComponentConfig{}
	if got := selection.Config().Components["worker"].Name; got != "first" {
		t.Fatalf("flow mutation changed selection: %q", got)
	}
}

func TestSelectBootRejectsStaticAndCrossFlowCollisionsDeterministically(t *testing.T) {
	static := &config.Config{Components: config.ComponentConfigs{"worker": {Name: "static"}}}
	_, err := SelectBoot(static, []*Flow{{ID: "z-flow", DesiredState: DesiredEnabled, DesiredComponents: componentSet(true, `{}`)}})
	var conflict *ComponentOwnershipConflictError
	if !errors.As(err, &conflict) || conflict.Component != "worker" || conflict.ExistingOwner != "static" || conflict.RequestedOwner != "flow:z-flow" {
		t.Fatalf("static conflict = %#v (%v)", conflict, err)
	}

	_, err = SelectBoot(&config.Config{}, []*Flow{
		{ID: "z-flow", DesiredState: DesiredEnabled, DesiredComponents: componentSet(true, `{}`)},
		{ID: "a-flow", DesiredState: DesiredDisabled, DesiredComponents: componentSet(false, `{}`)},
	})
	if !errors.As(err, &conflict) || conflict.ExistingOwner != "flow:a-flow" || conflict.RequestedOwner != "flow:z-flow" {
		t.Fatalf("cross-flow conflict = %#v (%v)", conflict, err)
	}
}

func TestObserveCanonicalizesFullDesiredBundleAndIgnoresAuthoringNodes(t *testing.T) {
	boot := &Flow{ID: "flow-a", DesiredState: DesiredEnabled, DesiredComponents: componentSet(true, `{"alpha":1,"beta":{"x":2,"y":3}}`)}
	selection, err := SelectBoot(&config.Config{}, []*Flow{boot})
	if err != nil {
		t.Fatal(err)
	}
	desired := &Flow{ID: "flow-a", DesiredState: DesiredEnabled, DesiredComponents: componentSet(true, "{\n\"beta\":{\"y\":3,\"x\":2},\"alpha\":1}"), Nodes: []FlowNode{{ID: "new-authoring-node"}}}
	observation := selection.Observe(desired)
	boolPointer(t, observation.RestartRequired, false)
}

func TestMarshalPersistedFlowOmitsRuntimeObservation(t *testing.T) {
	restart := true
	flow := &Flow{
		ID: "flow-a", Name: "Flow A", DesiredState: DesiredEnabled, DesiredComponents: componentSet(true, `{}`),
		EffectiveState: EffectiveEnabled, RestartRequired: &restart,
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
	if _, ok := persisted["desired_components"]; !ok {
		t.Fatalf("persisted flow omits desired bundle: %s", data)
	}
}

func TestBootIDsAreUnique(t *testing.T) {
	if first, second := newBootID(), newBootID(); first == second {
		t.Fatalf("boot IDs are not unique: %q", first)
	}
}
