package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/c360studio/semstreams/types"
)

// CompositionSealedError reports an attempted composition write after StartAll
// fixed the process service identity set.
type CompositionSealedError struct {
	Operation string
	Name      string
}

func (e *CompositionSealedError) Error() string {
	return fmt.Sprintf("%s service %s: service composition is sealed", e.Operation, e.Name)
}

// DuplicateServiceError reports two composition writers claiming one service
// map-key identity.
type DuplicateServiceError struct {
	Name string
}

func (e *DuplicateServiceError) Error() string {
	return fmt.Sprintf("service %s is already registered", e.Name)
}

// MandatoryServiceDisabledError reports desired state that cannot form a valid
// framework process composition.
type MandatoryServiceDisabledError struct {
	Name string
}

func (e *MandatoryServiceDisabledError) Error() string {
	return fmt.Sprintf("mandatory service %s cannot be disabled", e.Name)
}

const (
	serviceChangeAdd         = "add"
	serviceChangeEnable      = "enable"
	serviceChangeDisable     = "disable"
	serviceChangeRemove      = "remove"
	serviceChangeReconfigure = "reconfigure"
)

// PendingServiceChange describes one structural desired-service change that a
// process restart is required to attempt to consume.
type PendingServiceChange struct {
	Name   string `json:"name"`
	Change string `json:"change"`
}

// ResolveServiceConfigs returns the deterministic outer desired-service map.
// It owns map structure and activation defaults only; service constructors
// remain the sole interpreters of inner configuration.
func ResolveServiceConfigs(configs types.ServiceConfigs) (types.ServiceConfigs, error) {
	return resolveServiceConfigs(configs, true)
}

func resolveServiceConfigs(configs types.ServiceConfigs, applyDefaults bool) (types.ServiceConfigs, error) {
	resolved := make(types.ServiceConfigs, len(configs)+3)
	for name, serviceConfig := range configs {
		canonical, err := canonicalServiceJSON(serviceConfig.Config)
		if err != nil {
			return nil, fmt.Errorf("service %s config: %w", name, err)
		}
		resolved[name] = types.ServiceConfig{
			Enabled: serviceConfig.Enabled,
			Config:  canonical,
		}
	}

	if !applyDefaults {
		return resolved, nil
	}

	materializeServiceConfig(resolved, "component-manager", json.RawMessage(`{}`))
	materializeServiceConfig(resolved, "service-manager", json.RawMessage(`{}`))
	// Metrics remains the existing default-on optional service. Explicit false
	// is preserved, so it is still an optional activation choice.
	materializeServiceConfig(resolved, "metrics", json.RawMessage(`{"path":"/metrics","port":9090}`))
	return resolved, nil
}

func materializeServiceConfig(configs types.ServiceConfigs, name string, raw json.RawMessage) {
	if _, exists := configs[name]; exists {
		return
	}
	configs[name] = types.ServiceConfig{Enabled: true, Config: bytes.Clone(raw)}
}

func canonicalServiceJSON(raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage(`{}`), nil
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("canonicalize JSON: %w", err)
	}
	return json.RawMessage(canonical), nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode JSON: multiple values")
		}
		return fmt.Errorf("decode JSON: %w", err)
	}
	return nil
}

func decodeStrictServiceJSON(raw json.RawMessage, target any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	return decodeStrictJSON(bytes.NewReader(raw), target)
}

func decodeStrictJSON(reader io.Reader, target any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

func cloneResolvedServiceConfigs(configs types.ServiceConfigs) types.ServiceConfigs {
	clone := make(types.ServiceConfigs, len(configs))
	for name, serviceConfig := range configs {
		clone[name] = types.ServiceConfig{
			Enabled: serviceConfig.Enabled,
			Config:  bytes.Clone(serviceConfig.Config),
		}
	}
	return clone
}

func pendingServiceChanges(boot, desired types.ServiceConfigs) []PendingServiceChange {
	names := make(map[string]struct{}, len(boot)+len(desired))
	for name := range boot {
		names[name] = struct{}{}
	}
	for name := range desired {
		names[name] = struct{}{}
	}

	orderedNames := make([]string, 0, len(names))
	for name := range names {
		orderedNames = append(orderedNames, name)
	}
	sort.Strings(orderedNames)

	changes := make([]PendingServiceChange, 0)
	for _, name := range orderedNames {
		bootConfig, existedAtBoot := boot[name]
		desiredConfig, desiredNow := desired[name]

		change := ""
		switch {
		case !existedAtBoot && desiredNow && desiredConfig.Enabled:
			change = serviceChangeAdd
		case existedAtBoot && !bootConfig.Enabled && desiredNow && desiredConfig.Enabled:
			change = serviceChangeEnable
		case existedAtBoot && bootConfig.Enabled && desiredNow && !desiredConfig.Enabled:
			change = serviceChangeDisable
		case existedAtBoot && bootConfig.Enabled && !desiredNow:
			change = serviceChangeRemove
		case existedAtBoot && bootConfig.Enabled && desiredNow && desiredConfig.Enabled &&
			!bytes.Equal(bootConfig.Config, desiredConfig.Config):
			change = serviceChangeReconfigure
		}
		if change != "" {
			changes = append(changes, PendingServiceChange{Name: name, Change: change})
		}
	}
	return changes
}
