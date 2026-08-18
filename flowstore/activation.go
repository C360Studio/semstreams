package flowstore

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/internal/jsoncanon"
	"github.com/c360studio/semstreams/types"
	"github.com/google/uuid"
)

// ComponentOwnershipConflictError reports an ambiguous boot composition.
type ComponentOwnershipConflictError struct {
	Component      string
	ExistingOwner  string
	RequestedOwner string
}

func (e *ComponentOwnershipConflictError) Error() string {
	return fmt.Sprintf("component %q is owned by %s and cannot also be owned by %s", e.Component, e.ExistingOwner, e.RequestedOwner)
}

// ActivationObservation is process-local evidence comparing durable desired
// activation with the immutable composition selected for this boot.
type ActivationObservation struct {
	EffectiveState        EffectiveState
	DesiredProvenance     *ConfigProvenance
	BootAppliedProvenance *ConfigProvenance
	RestartRequired       *bool
}

type bootFlowActivation struct {
	state      DesiredState
	components DesiredComponentSet
}

// BootSelection is the immutable, composition-root-owned runtime selection.
type BootSelection struct {
	config *config.Config
	bootID string
	flows  map[string]bootFlowActivation
}

// SelectBoot selects the exact runtime composition once, after configuration
// arbitration and before any component construction.
func SelectBoot(effective *config.Config, flows []*Flow) (*BootSelection, error) {
	if effective == nil {
		return nil, fmt.Errorf("effective config cannot be nil")
	}
	selected := effective.Clone()
	if selected.Components == nil {
		selected.Components = make(config.ComponentConfigs)
	}
	owners := make(map[string]string, len(selected.Components))
	for name := range selected.Components {
		owners[name] = "static"
	}

	sorted := append([]*Flow(nil), flows...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	bootFlows := make(map[string]bootFlowActivation, len(sorted))
	for _, flow := range sorted {
		if flow == nil || flow.ID == "" {
			return nil, fmt.Errorf("boot flow identity cannot be empty")
		}
		if _, exists := bootFlows[flow.ID]; exists {
			return nil, fmt.Errorf("duplicate boot flow %q", flow.ID)
		}
		bundle := cloneDesiredComponentSet(flow.DesiredComponents)
		if err := validateDesiredActivation(flow.DesiredState, bundle); err != nil {
			return nil, fmt.Errorf("flow %q: %w", flow.ID, err)
		}
		bootFlows[flow.ID] = bootFlowActivation{state: flow.DesiredState, components: bundle}
		if flow.DesiredState == DesiredAbsent {
			continue
		}
		names := sortedComponentNames(bundle)
		for _, name := range names {
			requested := "flow:" + flow.ID
			if existing, exists := owners[name]; exists {
				return nil, &ComponentOwnershipConflictError{Component: name, ExistingOwner: existing, RequestedOwner: requested}
			}
			owners[name] = requested
			selected.Components[name] = cloneComponentConfig(bundle[name])
		}
	}
	return &BootSelection{config: selected, bootID: newBootID(), flows: bootFlows}, nil
}

// Config returns a defensive copy of the selected runtime configuration.
func (s *BootSelection) Config() *config.Config {
	if s == nil || s.config == nil {
		return nil
	}
	return s.config.Clone()
}

// Observe compares one durable flow with this boot's immutable selection.
func (s *BootSelection) Observe(flow *Flow) ActivationObservation {
	unknown := ActivationObservation{EffectiveState: EffectiveUnknown}
	if s == nil || flow == nil || s.bootID == "" {
		return unknown
	}
	boot, ok := s.flows[flow.ID]
	if !ok {
		boot = bootFlowActivation{state: DesiredAbsent, components: DesiredComponentSet{}}
	}
	desiredBundle := cloneDesiredComponentSet(flow.DesiredComponents)
	desiredDigest := digestActivation(flow.ID, flow.DesiredState, desiredBundle)
	bootDigest := digestActivation(flow.ID, boot.state, boot.components)
	restart := desiredDigest != bootDigest
	return ActivationObservation{
		EffectiveState:        effectiveState(boot.state),
		DesiredProvenance:     &ConfigProvenance{Digest: desiredDigest},
		BootAppliedProvenance: &ConfigProvenance{BootID: s.bootID, Digest: bootDigest},
		RestartRequired:       &restart,
	}
}

// Decorate applies process-local activation evidence to a detached flow value.
func (s *BootSelection) Decorate(flow *Flow) {
	if flow == nil {
		return
	}
	observation := s.Observe(flow)
	flow.EffectiveState = observation.EffectiveState
	flow.DesiredProvenance = observation.DesiredProvenance
	flow.BootAppliedProvenance = observation.BootAppliedProvenance
	flow.RestartRequired = observation.RestartRequired
}

func validateDesiredActivation(state DesiredState, components DesiredComponentSet) error {
	switch state {
	case DesiredAbsent:
		if len(components) != 0 {
			return fmt.Errorf("absent activation must have an empty desired component set")
		}
	case DesiredDisabled, DesiredEnabled:
		if len(components) == 0 {
			return fmt.Errorf("%s activation must have a non-empty desired component set", state)
		}
		wantEnabled := state == DesiredEnabled
		for _, name := range sortedComponentNames(components) {
			if components[name].Enabled != wantEnabled {
				return fmt.Errorf("component %q enabled=%t does not match desired state %s", name, components[name].Enabled, state)
			}
		}
	default:
		return fmt.Errorf("invalid desired state %q", state)
	}
	return nil
}

func effectiveState(state DesiredState) EffectiveState {
	switch state {
	case DesiredAbsent:
		return EffectiveAbsent
	case DesiredDisabled:
		return EffectiveDisabled
	case DesiredEnabled:
		return EffectiveEnabled
	default:
		return EffectiveUnknown
	}
}

func digestActivation(flowID string, state DesiredState, components DesiredComponentSet) string {
	type digestComponent struct {
		Name   string                `json:"instance"`
		Config types.ComponentConfig `json:"config"`
	}
	payload := struct {
		FlowID     string            `json:"flow_id"`
		State      DesiredState      `json:"desired_state"`
		Components []digestComponent `json:"components"`
	}{FlowID: flowID, State: state, Components: make([]digestComponent, 0, len(components))}
	for _, name := range sortedComponentNames(components) {
		componentConfig := cloneComponentConfig(components[name])
		if canonical, valid := jsoncanon.Normalize(componentConfig.Config); valid {
			componentConfig.Config = canonical
		}
		payload.Components = append(payload.Components, digestComponent{Name: name, Config: componentConfig})
	}
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("sha256:%x", digest[:])
}

func sortedComponentNames(components DesiredComponentSet) []string {
	names := make([]string, 0, len(components))
	for name := range components {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func cloneDesiredComponentSet(source DesiredComponentSet) DesiredComponentSet {
	result := make(DesiredComponentSet, len(source))
	for name, componentConfig := range source {
		result[name] = cloneComponentConfig(componentConfig)
	}
	return result
}

func cloneComponentConfig(source types.ComponentConfig) types.ComponentConfig {
	cloned := source
	cloned.Config = append(json.RawMessage(nil), source.Config...)
	return cloned
}

func newBootID() string {
	return uuid.NewString()
}
