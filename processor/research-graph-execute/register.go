package researchexecute

import "github.com/c360studio/semstreams/component"

// Register registers the execute_subqueries processor with the
// supplied component registry. Called from componentregistry.Register
// at process bootstrap so production binaries pick the component up
// without extra wiring.
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        ComponentName,
		Factory:     NewProcessor,
		Schema:      configSchema,
		Type:        "processor",
		Protocol:    "execute_subqueries",
		Domain:      "research",
		Description: "ADR-045 execute_subqueries: materialise sub-queries from RouteDecision intent + fan-out across Tier 0+1 retrieval + dedup + rank + budget. Emits ExecutionOutput for R3.",
		Version:     "0.1.0",
	})
}
