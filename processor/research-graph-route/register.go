package researchroute

import "github.com/c360studio/semstreams/component"

// Register registers the route_search processor with the supplied
// component registry. Called from componentregistry.Register at
// process bootstrap so production binaries pick the component up
// without extra wiring.
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        ComponentName,
		Factory:     NewProcessor,
		Schema:      configSchema,
		Type:        "processor",
		Protocol:    "route_search",
		Domain:      "research",
		Description: "ADR-045 route_search: structured-emit routing decision over upstream ClassifierOutput. Emits one of synthesize_directly / retighten / walk_seeds / decompose for R2 dispatch.",
		Version:     "0.1.0",
	})
}
