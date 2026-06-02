package researchsynthesize

import "github.com/c360studio/semstreams/component"

// Register registers the synthesize_answer processor with the
// supplied component registry. Called from
// componentregistry.Register at process bootstrap.
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        ComponentName,
		Factory:     NewProcessor,
		Schema:      configSchema,
		Type:        "processor",
		Protocol:    "synthesize_answer",
		Domain:      "research",
		Description: "ADR-045 synthesize_answer: terminal LLM stage. Quote-back-validated synthesis grounded in upstream evidence. Drives the continuation rule.",
		Version:     "0.1.0",
	})
}
