package researchclassify

import "github.com/c360studio/semstreams/component"

// Register registers the nl_classify processor with the supplied
// component registry. Called from componentregistry.Register at
// process bootstrap so production binaries pick the component up
// without extra wiring.
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        ComponentName,
		Factory:     NewProcessor,
		Schema:      configSchema,
		Type:        "processor",
		Protocol:    "nl_classify",
		Domain:      "research",
		Description: "ADR-045 nl_classify: classify a research topic via the existing graph/query.ClassifierChain and surface initial candidate entities for downstream route_search.",
		Version:     "0.1.0",
	})
}
