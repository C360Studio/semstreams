package composition

import (
	"errors"
	"fmt"
	"sort"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/types"
)

// Validate is the offline judgment of a composition: every enabled component
// in cfg is checked against the catalog exactly as boot admission would check
// it — name, type, factory, raw-config security, the factory's port declarer,
// exclusive resources — and the declared ports are then analyzed as a graph.
// It performs no I/O, opens no connection, and constructs no component; the
// only error return is for nil arguments, every judgment is a Finding.
func Validate(catalog *component.Registry, cfg *config.Config) (*Result, error) {
	if catalog == nil {
		return nil, errors.New("composition: nil catalog")
	}
	if cfg == nil {
		return nil, errors.New("composition: nil configuration")
	}
	result := newResult()

	// Validate normalizes in place (platform.org is lowercased), so judge a
	// clone and leave the caller's document untouched.
	working := cfg.Clone()
	if err := working.Validate(); err != nil {
		result.add(Finding{
			Type: TypeConfigInvalid, Severity: severityOf(TypeConfigInvalid, nil),
			Component: "config", Message: err.Error(),
			Suggestions: []string{"Fix the configuration document; component-level findings below are still reported"},
		})
	}

	factories := catalog.ListFactories()
	names := make([]string, 0, len(working.Components))
	for name := range working.Components {
		names = append(names, name)
	}
	sort.Strings(names)

	declarations := make([]component.Declaration, 0, len(names))
	claimed := map[string]string{} // exclusive resource → instance that holds it
	for _, name := range names {
		entry := working.Components[name]
		if !entry.Enabled {
			continue
		}
		declaration, finding := declareInstance(catalog, factories, name, entry)
		if finding != nil {
			result.add(*finding)
			continue
		}
		if conflict := exclusiveConflict(claimed, declaration); conflict != nil {
			result.add(*conflict)
			continue // boot admission refuses the second holder; it never reaches the graph
		}
		for _, resource := range declaration.ExclusiveResources {
			claimed[resource] = name
		}
		declarations = append(declarations, declaration)
	}

	analysis := Analyze(declarations, working.Streams)
	result.Errors = append(result.Errors, analysis.Errors...)
	result.Warnings = append(result.Warnings, analysis.Warnings...)
	result.Graph = analysis.Graph
	result.finalize()
	return result, nil
}

// declareInstance mirrors the checks boot admission performs before a factory
// runs (component/registry.go prepareComponent), classifying each refusal as a
// finding, then evaluates the declarer.
func declareInstance(
	catalog *component.Registry, factories map[string]*component.Registration,
	name string, entry types.ComponentConfig,
) (component.Declaration, *Finding) {
	invalid := func(err error) *Finding {
		return &Finding{
			Type: TypeComponentConfigInvalid, Severity: severityOf(TypeComponentConfigInvalid, nil),
			Component: name, Message: err.Error(),
			Suggestions: []string{"Fix the component entry: a valid instance name, type, factory name, and JSON config"},
		}
	}
	if err := component.ValidateComponentName(name); err != nil {
		return component.Declaration{}, invalid(fmt.Errorf("instance name: %w", err))
	}
	if err := entry.Validate(); err != nil {
		return component.Declaration{}, invalid(err)
	}
	if err := component.ValidateComponentName(entry.Name); err != nil {
		return component.Declaration{}, invalid(fmt.Errorf("factory name: %w", err))
	}
	registration, exists := factories[entry.Name]
	if !exists {
		return component.Declaration{}, &Finding{
			Type: TypeUnknownComponent, Severity: severityOf(TypeUnknownComponent, nil),
			Component: name,
			Message:   fmt.Sprintf("Unknown component: %s", entry.Name),
			Suggestions: []string{
				"Check that component is registered",
				"Verify component name spelling",
			},
		}
	}
	if registration.Type != string(entry.Type) {
		return component.Declaration{}, &Finding{
			Type: TypeComponentTypeMismatch, Severity: severityOf(TypeComponentTypeMismatch, nil),
			Component: name,
			Message:   fmt.Sprintf("component '%s' is type '%s', not '%s'", entry.Name, registration.Type, entry.Type),
			Suggestions: []string{
				fmt.Sprintf("Set \"type\": %q for factory %s", registration.Type, entry.Name),
			},
		}
	}
	if err := component.ValidateFactoryConfig(entry.Config); err != nil {
		return component.Declaration{}, invalid(err)
	}
	declaration, err := catalog.Declare(name, entry)
	if err != nil {
		return component.Declaration{}, &Finding{
			Type: TypePortDeclarationError, Severity: severityOf(TypePortDeclarationError, nil),
			Component: name,
			Message:   fmt.Sprintf("factory %s rejected the configuration: %v", entry.Name, err),
			Suggestions: []string{
				"Fix the component's config so its factory can declare ports",
				"Run `catalog` to see the factory's schema and default ports",
			},
		}
	}
	return declaration, nil
}

func exclusiveConflict(claimed map[string]string, declaration component.Declaration) *Finding {
	for _, resource := range declaration.ExclusiveResources {
		holder, taken := claimed[resource]
		if !taken {
			continue
		}
		return &Finding{
			Type: TypeExclusiveResourceConflict, Severity: severityOf(TypeExclusiveResourceConflict, nil),
			Component: declaration.InstanceName,
			Message:   fmt.Sprintf("resource conflict: %s already used by component '%s'", resource, holder),
			Suggestions: []string{
				"Give each component its own exclusive resource (address, port)",
				fmt.Sprintf("Or disable one of %s and %s", holder, declaration.InstanceName),
			},
		}
	}
	return nil
}
