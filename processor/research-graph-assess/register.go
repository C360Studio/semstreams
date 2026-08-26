package researchassess

import "github.com/c360studio/semstreams/component"

// Register registers the assess_sufficiency processor with the
// supplied component registry. Called from
// componentregistry.Register at process bootstrap so production
// binaries pick the component up without extra wiring.
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        ComponentName,
		Factory:     NewProcessor,
		Ports:       DeclarePorts,
		Schema:      configSchema,
		Type:        "processor",
		Protocol:    "assess_sufficiency",
		Domain:      "research",
		Description: "ADR-045 assess_sufficiency: structured-emit sufficient/refine decision over upstream ExecutionOutput evidence. Drives R3's synthesize-or-refine branch.",
		Version:     "0.1.0",
	})
}
